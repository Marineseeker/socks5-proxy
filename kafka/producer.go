package kafka

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"

	"github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *kafka.Writer
}

type KafkaConfig struct {
	Brokers   []string `yaml:"brokers"`
	Topic     string   `yaml:"topic"`
	Async     bool     `yaml:"async"`
	QueueSize int      `yaml:"queue_size"`
}

type Config struct {
	Kafka KafkaConfig `yaml:"kafka"`
}

func loadKafkaConfig() (*KafkaConfig, error) {
	configByte, err := os.ReadFile("config.yaml")
	if err != nil {
		return nil, fmt.Errorf(
			"failed to read config.yaml: %w",
			err,
		)
	}
	var config Config
	err = yaml.Unmarshal(configByte, &config)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to unmarshal config.yaml: %w",
			err,
		)
	}
	if len(config.Kafka.Brokers) == 0 {
		return nil, fmt.Errorf(
			"brokers is required in config.yaml",
		)
	}
	if config.Kafka.Topic == "" {
		return nil, fmt.Errorf(
			"kafka topic is empty",
		)
	}
	if config.Kafka.QueueSize <= 0 {
		config.Kafka.QueueSize = 1000
	}
	return &config.Kafka, nil
}

func NewProducer() (*Producer, error) {
	cfg, err := loadKafkaConfig()
	if err != nil {
		return nil, err
	}
	writer := &kafka.Writer{
		Addr:     kafka.TCP(cfg.Brokers...),
		Topic:    cfg.Topic,
		Balancer: &kafka.LeastBytes{},
	}
	return &Producer{
		writer: writer,
	}, nil
}

func (p *Producer) Send(
	ctx context.Context,
	key []byte,
	value []byte,
) error {
	return p.writer.WriteMessages(
		ctx,
		kafka.Message{
			Key:   key,
			Value: value,
		},
	)
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
