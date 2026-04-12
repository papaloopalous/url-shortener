package kafka

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *kafka.Writer
}

func NewProducer(brokers []string) *Producer {
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Balancer:               &kafka.Hash{}, // партиционирование по ключу
		AllowAutoTopicCreation: true,
		RequiredAcks:           kafka.RequireOne,
	}
	return &Producer{writer: writer}
}

func (p *Producer) Publish(ctx context.Context, topic string, key, value []byte) error {
	msg := kafka.Message{
		Topic: topic,
		Key:   key,
		Value: value,
	}
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}
	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
