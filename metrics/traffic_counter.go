package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// ClientTrafficMap 维护客户端IP到流量计数器的映射
type ClientTrafficMap struct {
	mu      sync.RWMutex
	clients map[string]*clientAtomicCounter
	// 清理相关
	lastCleanup     time.Time
	cleanupInterval time.Duration
	maxInactive     time.Duration
}

// clientAtomicCounter 使用原子操作的客户端计数器
type clientAtomicCounter struct {
	upload     uint64
	download   uint64
	lastActive int64                // Unix timestamp, atomic
	_          [64 - (24 % 64)]byte // 填充到 64 字节，避免伪共享
}

var (
	clientTrafficMap = &ClientTrafficMap{
		clients:         make(map[string]*clientAtomicCounter),
		cleanupInterval: 5 * time.Minute,
		maxInactive:     30 * time.Minute,
	}
)

// AddTraffic 累加客户端流量（线程安全）
func AddTraffic(clientIP string, isUpload bool, bytes uint64) {
	// 获取或创建客户端计数器
	c := clientTrafficMap.getOrCreateCounter(clientIP)

	// 原子累加
	if isUpload {
		atomic.AddUint64(&c.upload, bytes)
	} else {
		atomic.AddUint64(&c.download, bytes)
	}

	// 更新最后活跃时间
	atomic.StoreInt64(&c.lastActive, time.Now().Unix())
}

// getOrCreateCounter 获取或创建客户端计数器
func (m *ClientTrafficMap) getOrCreateCounter(clientIP string) *clientAtomicCounter {
	// 先尝试读锁
	m.mu.RLock()
	counter, ok := m.clients[clientIP]
	m.mu.RUnlock()

	if ok {
		return counter
	}

	// 需要创建新计数器，加写锁
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if counter, ok := m.clients[clientIP]; ok {
		return counter
	}

	counter = &clientAtomicCounter{
		upload:     0,
		download:   0,
		lastActive: time.Now().Unix(),
	}
	m.clients[clientIP] = counter
	return counter
}

// CollectAndReset 收集所有客户端流量数据并重置计数器
func (m *ClientTrafficMap) CollectAndReset() []*TrafficMetric {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().Unix()
	metrics := make([]*TrafficMetric, 0, len(m.clients))

	for ip, counter := range m.clients {
		// 使用 Swap 原子操作获取并清零
		upload := atomic.SwapUint64(&counter.upload, 0)
		download := atomic.SwapUint64(&counter.download, 0)

		// 只记录有流量的客户端
		if upload > 0 || download > 0 {
			metrics = append(metrics, &TrafficMetric{
				Timestamp:     now,
				ClientIP:      ip,
				UploadBytes:   upload,
				DownloadBytes: download,
			})
		}
	}

	// 定期清理不活跃客户端
	m.cleanupInactive(now)

	return metrics
}

// cleanupInactive 清理长时间不活跃的客户端
func (m *ClientTrafficMap) cleanupInactive(now int64) {
	if now-m.lastCleanup.Unix() < int64(m.cleanupInterval.Seconds()) {
		return
	}
	m.lastCleanup = time.Unix(now, 0)

	threshold := now - int64(m.maxInactive.Seconds())

	for ip, counter := range m.clients {
		lastActive := atomic.LoadInt64(&counter.lastActive)
		if lastActive < threshold {
			delete(m.clients, ip)
		}
	}
}

// GetClientCount 返回当前活跃客户端数量
func GetClientCount() int {
	clientTrafficMap.mu.RLock()
	defer clientTrafficMap.mu.RUnlock()
	return len(clientTrafficMap.clients)
}

// SetCleanupConfig 设置清理配置
func SetCleanupConfig(cleanupInterval, maxInactive time.Duration) {
	clientTrafficMap.mu.Lock()
	defer clientTrafficMap.mu.Unlock()
	clientTrafficMap.cleanupInterval = cleanupInterval
	clientTrafficMap.maxInactive = maxInactive
}
