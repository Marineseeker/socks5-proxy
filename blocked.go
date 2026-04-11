package main

import (
	"bufio"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type blockEntry struct {
	failures     int
	blockedUntil time.Time
	permanent    bool
}

type blockedCache struct {
	mu       sync.RWMutex
	m        map[string]*blockEntry
	ttl      time.Duration
	filePath string
}

func newBlockedCache(ttl time.Duration, filePath string) *blockedCache {
	bc := &blockedCache{
		m:        make(map[string]*blockEntry),
		ttl:      ttl,
		filePath: filePath,
	}
	bc.loadPermanent()
	return bc
}

func (bc *blockedCache) loadPermanent() {
	f, err := os.Open(bc.filePath)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		addr := strings.TrimSpace(scanner.Text())
		if addr != "" {
			bc.m[addr] = &blockEntry{permanent: true}
		}
	}
	zap.S().Infof("[socks] loaded %d permanently blocked addresses", len(bc.m))
}

func (bc *blockedCache) savePermanent(addr string) {
	f, err := os.OpenFile(bc.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		zap.S().Errorf("failed to save blocked addr: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(addr + "\n"); err != nil {
		zap.S().Errorf("failed to write blocked addr into file: %v", err)
	}
}

func (bc *blockedCache) isBlocked(addr string) bool {
	bc.mu.RLock()
	e, ok := bc.m[addr]
	bc.mu.RUnlock()
	if !ok {
		return false
	}
	if e.permanent {
		return true
	}
	if e.failures < 3 {
		return false
	}
	if time.Now().Before(e.blockedUntil) {
		return true
	}
	return false
}

func (bc *blockedCache) markBlocked(addr string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	entry, ok := bc.m[addr]
	if !ok {
		entry = &blockEntry{}
		bc.m[addr] = entry
	}
	if entry.permanent {
		return
	}
	entry.failures++
	if entry.failures <= 3 {
		entry.blockedUntil = time.Now().Add(bc.ttl)
		zap.S().Infof("[socks] %s is temporarily blocked until %s", addr, entry.blockedUntil)
		return
	}
	if entry.failures > 3 {
		entry.permanent = true
		bc.savePermanent(addr)
		zap.S().Infof("[socks] %s is permanently blocked after %d failures", addr, entry.failures)
	}
}

func (bc *blockedCache) unmarkBlocked(addr string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if e, ok := bc.m[addr]; ok && !e.permanent {
		delete(bc.m, addr)
	}
}
