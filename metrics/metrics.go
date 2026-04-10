package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/socks5-proxy/user"
)

// 全局原子计数，记录经过代理的总流量（字节）
var (
	totalUpload   uint64
	totalDownload uint64
)

func GetTotals() (uint64, uint64) {
	return atomic.LoadUint64(&totalUpload), atomic.LoadUint64(&totalDownload)
}

type Point struct {
	Ts       int64  `json:"ts"`     // unix timestamp
	Upload   uint64 `json:"upload"` // bytes in this interval
	Download uint64 `json:"download"`
}

var (
	seriesMu  sync.Mutex
	series    []Point
	maxPoints = 720 // 保留最近 720 个点（约 1 小时，5s/点）
)

// AddToTotals 在转发时调用以累加全局计数
func AddToTotals(isUpload bool, n uint64) {
	if isUpload {
		atomic.AddUint64(&totalUpload, n)
	} else {
		atomic.AddUint64(&totalDownload, n)
	}
}

// StartMetricsAggregator 启动一个后台协程，每 interval 把增量上报到 currentUser 并记录时间序列点
func StartMetricsAggregator(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var lastU, lastD uint64
		for range ticker.C {
			u := atomic.LoadUint64(&totalUpload)
			d := atomic.LoadUint64(&totalDownload)
			deltaU := u
			deltaD := d
			if u > lastU {
				deltaU = u - lastU
			} else {
				deltaU = 0
			}
			if d > lastD {
				deltaD = d - lastD
			} else {
				deltaD = 0
			}
			lastU = u
			lastD = d

			// 上报到 user 单例的流量统计（只从这里上报）
			if deltaU > 0 || deltaD > 0 {
				cu := user.GetCurrentUser()
				if deltaU > 0 {
					cu.AddUpload(deltaU)
				}
				if deltaD > 0 {
					cu.AddDownload(deltaD)
				}
			}

			// 记录时间序列点（以 delta 为单位）
			pt := Point{Ts: time.Now().Unix(), Upload: deltaU, Download: deltaD}
			seriesMu.Lock()
			series = append(series, pt)
			if len(series) > maxPoints {
				// 保持尾部最新数据
				series = series[len(series)-maxPoints:]
			}
			seriesMu.Unlock()
		}
	}()
}

// GetSeries 返回当前时间序列的副本（按时间升序）
func GetSeries() []Point {
	seriesMu.Lock()
	defer seriesMu.Unlock()
	out := make([]Point, len(series))
	copy(out, series)
	return out
}

// GetLastNPoint 返回最近 n 个时间序列点（按时间升序）
func GetLastNPoint(n int) []Point {
	seriesMu.Lock()
	defer seriesMu.Unlock()

	if n > len(series) {
		n = len(series)
	}
	out := make([]Point, n)
	copy(out, series[len(series)-n:])
	return out
}

func GetLastPoint() Point {
	pts := GetLastNPoint(1)
	if len(pts) == 0 {
		return Point{Ts: time.Now().Unix(), Upload: 0, Download: 0}
	}
	return pts[0]
}
