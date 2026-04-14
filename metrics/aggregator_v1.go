package metrics

import (
	"sync/atomic"
	"time"

	"github.com/socks5-proxy/user"
)

// StartMetricsAggregatorV1 启动一个后台协程，每 interval 把增量上报到 currentUser 并记录时间序列点
func StartMetricsAggregatorV1(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastU, lastD uint64
	for range ticker.C {
		U := atomic.LoadUint64(&totalUpload)
		D := atomic.LoadUint64(&totalDownload)

		deltaU := U - lastU
		deltaD := D - lastD
		if U > lastU {
			deltaU = U - lastU
		} else {
			deltaU = 0
		}
		if D > lastD {
			deltaD = D - lastD
		} else {
			deltaD = 0
		}
		lastU = U
		lastD = D

		if deltaU > 0 || deltaD > 0 {
			cu := user.GetCurrentUser()
			if deltaU > 0 {
				cu.AddUpload(deltaU)
			}
			if deltaD > 0 {
				cu.AddDownload(deltaD)
			}
		}
		pt := Point{
			Ts:            time.Now().Unix(),
			UploadSpeed:   deltaU,
			DownloadSpeed: deltaD,
		}
		seriesMu.Lock()
		series = append(series, pt)
		if len(series) > maxPoints {
			series = series[len(series)-maxPoints:]
		}
		seriesMu.Unlock()
		broadcast(pt)
	}
}
