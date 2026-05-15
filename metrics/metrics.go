package metrics

import (
	"sort"
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

// GetSeriesSince 返回时间戳大于 since 的时间序列点（按时间升序）
// 使用二分查找定位起始位置，避免全量拷贝后线性过滤
func GetSeriesSince(since int64) []Point {
	seriesMu.Lock()
	defer seriesMu.Unlock()

	// sort.Search 找到第一个 Ts > since 的索引
	idx := sort.Search(len(series), func(i int) bool {
		return series[i].Ts > since
	})

	if idx >= len(series) {
		return nil
	}
	out := make([]Point, len(series)-idx)
	copy(out, series[idx:])
	return out
}

func GetLastPoint() *Point {
	pt := GetLastNPoint(1)
	if len(series) == 0 {
		return &Point{
			Ts:            time.Now().Unix(),
			UploadSpeed:   0,
			DownloadSpeed: 0,
		}
	}
	return &pt[0]
}
