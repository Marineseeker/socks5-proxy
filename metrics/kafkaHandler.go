package metrics

import (
	"context"
	"encoding/json"

	"github.com/socks5-proxy/kafka"
	"go.uber.org/zap"
)

type KafkaHandler struct {
	producer *kafka.Producer
	ch       chan *RawTrafficEvent
}

func NewKafkaHandler() (*KafkaHandler, error) {
	producer, err := kafka.NewProducer()
	if err != nil {
		return nil, err
	}
	return &KafkaHandler{
		producer: producer,
		ch:       Subscribe(),
	}, nil
}

func (h *KafkaHandler) Start() {
	zap.S().Info("KafkaHandler started")

	for event := range h.ch {
		data, err := json.Marshal(event)
		if err != nil {
			zap.S().Errorf("failed to marshal event: %v", err)
			continue
		}
		if err := h.producer.Send(context.Background(), []byte(event.ClientIP), data); err != nil {
			zap.S().Errorf("failed to send event to Kafka: %v", err)
			continue
		}
		zap.S().Debugf("sent RawTrafficEvent to Kafka: client=%s, bytes=%d, upload=%t",
			event.ClientIP, event.ByteCount, event.IsUpload)
	}
}

func StartKafkaHandler() error {
	handler, err := NewKafkaHandler()
	if err != nil {
		return err
	}
	handler.Start()
	return nil
}

func (h *KafkaHandler) Stop() {
	Unsubscribe(h.ch)
	h.producer.Close()
	zap.S().Info("Kafka handler stopped")
}
