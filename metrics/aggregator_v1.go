package metrics

import (
	"sync/atomic"
	"time"
)

// StartMetricsAggregatorV1 启动一个后台协程，每 interval 把增量上报到 currentUser 并记录时间序列点
func StartRawTrafficAggregator(interval time.Duration) {
	ch := Subscribe()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer Unsubscribe(ch)

		var lastTime time.Time
		var deltaU, deltaD uint64

		for {
			select {
			case event := <-ch:
				if event.IsUpload {
					deltaU += event.ByteCount
				} else {
					deltaD += event.ByteCount
				}

			case <-ticker.C:
				now := time.Now()
				elapsed := now.Sub(lastTime).Seconds()
				if elapsed <= 0 {
					elapsed = interval.Seconds()
				}
				lastTime = now

				// 更新全局计数器（用于 GetTotals）
				atomic.AddUint64(&totalUpload, deltaU)
				atomic.AddUint64(&totalDownload, deltaD)

				// 计算并存储 Point
				pt := Point{
					Ts:            now.Unix(),
					UploadSpeed:   uint64(float64(deltaU) / elapsed),
					DownloadSpeed: uint64(float64(deltaD) / elapsed),
				}

				seriesMu.Lock()
				series = append(series, pt)
				if len(series) > maxPoints {
					series = series[len(series)-maxPoints:]
				}
				seriesMu.Unlock()

				deltaU = 0
				deltaD = 0
			}
		}
	}()
}
