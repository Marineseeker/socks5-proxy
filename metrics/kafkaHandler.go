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

func (h *KafkaHandler) start(ctx context.Context) {
	zap.S().Info("KafkaHandler started")
	defer h.stop()

	for {
		select {
		case <-ctx.Done():
			zap.S().Info("KafkaHandler stopping due to context cancellation")
			return
		case event, ok := <-h.ch:
			if !ok {
				zap.S().Info("KafkaHandler channel closed")
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				zap.S().Errorf("failed to marshal event: %v", err)
				continue
			}
			if err := h.producer.Send(ctx, []byte(event.ClientIP), data); err != nil {
				if ctx.Err() != nil {
					return // Context was canceled, exit gracefully
				}
				zap.S().Errorf("failed to send event to Kafka: %v", err)
				continue
			}
			zap.S().Infof("sent RawTrafficEvent to Kafka: client=%s, bytes=%d, upload=%t",
				event.ClientIP, event.ByteCount, event.IsUpload)
		}
	}
}

func StartKafkaHandler() error {
	handler, err := NewKafkaHandler()
	if err != nil {
		return err
	}
	handler.start(context.Background())
	return nil
}

func (h *KafkaHandler) stop() {
	Unsubscribe(h.ch)
	h.producer.Close()
	zap.S().Info("Kafka handler stopped")
}
