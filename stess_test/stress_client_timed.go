// socks_stress.go
// Linux-only: 通过 /proc/<pid> 读取 socks5 服务的 CPU / 内存占用
// 用法示例：
//   go run socks_stress.go -proxy 127.0.0.1:7890 -pid 1234 -target http://example.com -timeout 5s
// 并发数会从 0 递增到 100，每一档都会发起等量请求并统计指标

package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

var (
	mode      = flag.String("mode", "ramp", "stress mode: ramp | burst")
	burstConc = flag.Int("burst", 100, "burst mode concurrency")
	proxyAddr = flag.String("proxy", "47.108.73.56:1080", "socks5 proxy address")
	pid       = flag.Int("pid", 0, "PID of socks5 service (Linux only, 0 = skip)")
	target    = flag.String("target", "http://example.com", "target url")
	timeout   = flag.Duration("timeout", 5*time.Second, "request timeout")
)

func main() {
	flag.Parse()

	zap.S().Infof("SOCKS5 Stress Test -> proxy=%s target=%s pid=%d", *proxyAddr, *target, *pid)
	zap.S().Info(strings.Repeat("-", 90))
	zap.S().Infof("%-8s %-8s %-8s %-14s %-14s %-14s",
		"concur", "sent", "ok", "avg_latency", "cpu(%)", "mem(MB)")

	switch *mode {
	case "burst":
		runOnce(*burstConc)

	default: // ramp
		for c := 0; c <= 100; c++ {
			runOnce(c)
		}
	}
}

func runOnce(concurrency int) {
	if concurrency == 0 {
		zap.S().Infof("%-8d %-8d %-8d %-14s %-14s %-14s",
			0, 0, 0, "-", readCPU(), readMem())
		return
	}

	dialer, err := proxy.SOCKS5("tcp", *proxyAddr, nil, proxy.Direct)
	if err != nil {
		zap.S().Errorf("create socks5 dialer failed: %v", err)
		return
	}

	transport := &http.Transport{
		Dial: dialer.Dial,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   *timeout,
	}

	var sent int64
	var ok int64
	var totalLatency int64

	wg := sync.WaitGroup{}
	wg.Add(concurrency)

	startAll := time.Now()

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			atomic.AddInt64(&sent, 1)

			start := time.Now()
			resp, err := client.Get(*target)
			lat := time.Since(start)
			atomic.AddInt64(&totalLatency, lat.Nanoseconds())

			if err == nil {
				resp.Body.Close()
				atomic.AddInt64(&ok, 1)
			}
		}()
	}

	wg.Wait()
	_ = startAll

	avg := time.Duration(0)
	if ok > 0 {
		avg = time.Duration(totalLatency / ok)
	}

	zap.S().Infof("%-8d %-8d %-8d %-14s %-14s %-14s",
		concurrency, sent, ok, avg, readCPU(), readMem())
}

// ================= Linux /proc helpers =================

func readCPU() string {
	if *pid == 0 {
		return "-"
	}
	// 读取 /proc/<pid>/stat 的 utime + stime
	statPath := filepath.Join("/proc", strconv.Itoa(*pid), "stat")
	data, err := os.ReadFile(statPath)
	if err != nil {
		return "err"
	}
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return "err"
	}
	utime, _ := strconv.ParseFloat(fields[13], 64)
	stime, _ := strconv.ParseFloat(fields[14], 64)
	// 这里不做精确百分比，只给出一个相对 CPU tick 值
	return fmt.Sprintf("%.0f", utime+stime)
}

func readMem() string {
	if *pid == 0 {
		return "-"
	}
	statusPath := filepath.Join("/proc", strconv.Itoa(*pid), "status")
	f, err := os.Open(statusPath)
	if err != nil {
		return "err"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				kb, _ := strconv.ParseFloat(parts[1], 64)
				return fmt.Sprintf("%.2f", kb/1024)
			}
		}
	}
	return "0"
}
