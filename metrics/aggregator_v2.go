package metrics

import (
	"sync/atomic"
	"time"

	"github.com/socks5-proxy/user"
)

// StartMetricsAggregatorV2 启动改进版的速率计算器（不会阻塞调用者）。
// - 处理计数器复位/溢出（遇到新值小于上次值时按复位处理）
// - 计算每秒速率并使用指数移动平均（EMA）平滑瞬时波动
// - 在后台 goroutine 中运行并复用 package 级别的 series/broadcast 等
// smoothingAlpha 范围 (0,1]，越大越响应瞬时变化；传 0 或 >1 时使用默认 0.3
func StartMetricsAggregatorV2(interval time.Duration, smoothingAlpha float64) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if smoothingAlpha <= 0 || smoothingAlpha > 1 {
		smoothingAlpha = 0.3
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastU, lastD uint64
	lastTime := time.Now()

	var smoothU, smoothD float64 // bytes/sec (EMA)

	for range ticker.C {
		now := time.Now()
		elapsed := now.Sub(lastTime).Seconds()
		if elapsed <= 0 {
			elapsed = interval.Seconds()
		}
		lastTime = now

		U := atomic.LoadUint64(&totalUpload)
		D := atomic.LoadUint64(&totalDownload)

		var deltaU uint64
		if U >= lastU {
			deltaU = U - lastU
		} else {
			// 假设计数器被重置或溢出，使用当前值作为增量（比直接置0更保守）
			deltaU = U
		}

		var deltaD uint64
		if D >= lastD {
			deltaD = D - lastD
		} else {
			deltaD = D
		}

		lastU = U
		lastD = D

		// 即时速率（bytes/sec）
		instU := float64(deltaU) / elapsed
		instD := float64(deltaD) / elapsed

		// EMA 平滑
		if smoothU == 0 && smoothD == 0 {
			smoothU = instU
			smoothD = instD
		} else {
			smoothU = smoothingAlpha*instU + (1.0-smoothingAlpha)*smoothU
			smoothD = smoothingAlpha*instD + (1.0-smoothingAlpha)*smoothD
		}

		// 将平滑后的速率转换为与现有 Point 兼容的数值：保持为 bytes/sec（更语义化）
		pt := Point{
			Ts:            now.Unix(),
			UploadSpeed:   uint64(smoothU),
			DownloadSpeed: uint64(smoothD),
		}

		// 上报给当前用户（仍以原始增量计入总用户流量）
		if deltaU > 0 || deltaD > 0 {
			cu := user.GetCurrentUser()
			if deltaU > 0 {
				cu.AddUpload(deltaU)
			}
			if deltaD > 0 {
				cu.AddDownload(deltaD)
			}
		}

		// 记录到序列并广播
		seriesMu.Lock()
		series = append(series, pt)
		if len(series) > maxPoints {
			series = series[len(series)-maxPoints:]
		}
		seriesMu.Unlock()
		broadcast(pt)
	}
}
