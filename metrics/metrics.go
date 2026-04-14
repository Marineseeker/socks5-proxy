package metrics

import (
	"sync"
	"sync/atomic"
	"time"
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
	Ts            int64  `json:"ts"`           // unix timestamp
	UploadSpeed   uint64 `json:"upload_speed"` // bytes in this interval
	DownloadSpeed uint64 `json:"download_speed"`
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

func GetLastPoint() *Point {
	pt := GetLastNPoint(1)
	seriesMu.Lock()
	defer seriesMu.Unlock()
	if len(series) == 0 {
		return &Point{
			Ts:            time.Now().Unix(),
			UploadSpeed:   0,
			DownloadSpeed: 0,
		}
	}
	return &pt[0]
}
