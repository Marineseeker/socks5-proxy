package metrics_v2

import (
	"context"
	"encoding/json"
	"time"

	"github.com/socks5-proxy/kafka"
	"go.uber.org/zap"
)

// Aggregator 流量聚合器
type Aggregator struct {
	producer *kafka.Producer
	interval time.Duration
	running  bool
}

var (
	defaultAggregator *Aggregator
)

// StartAggregator 启动流量聚合器
// interval: 聚合周期，默认 1 秒
func StartAggregator(interval time.Duration) error {
	if interval <= 0 {
		interval = 1 * time.Second
	}

	producer, err := kafka.NewProducer()
	if err != nil {
		return err
	}

	defaultAggregator = &Aggregator{
		producer: producer,
		interval: interval,
		running:  true,
	}

	go defaultAggregator.run()

	zap.S().Infof("Aggregator started with interval %v", interval)
	return nil
}

// run 聚合器主循环
func (a *Aggregator) run() {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for range ticker.C {
		a.collectAndSend()
	}
}

// collectAndSend 收集流量数据并发送到 Kafka
func (a *Aggregator) collectAndSend() {
	// 收集所有客户端流量
	metrics := clientTrafficMap.CollectAndReset()

	if len(metrics) == 0 {
		return
	}

	// 批量发送到 Kafka
	for _, m := range metrics {
		data, err := json.Marshal(m)
		if err != nil {
			zap.S().Errorf("failed to marshal TrafficMetric: %v", err)
			continue
		}

		// 使用客户端IP作为 Kafka key，保证相同IP的消息落在同一分区
		err = a.producer.SendString(context.Background(), m.ClientIP, data)
		if err != nil {
			zap.S().Errorf("failed to send to Kafka: client=%s, err=%v", m.ClientIP, err)
		} else {
			zap.S().Debugf("sent TrafficMetric to Kafka: client=%s, upload=%d, download=%d",
				m.ClientIP, m.UploadBytes, m.DownloadBytes)
		}
	}
}
