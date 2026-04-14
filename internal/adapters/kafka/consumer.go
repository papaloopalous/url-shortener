package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	reader *kafka.Reader
}

func NewConsumer(brokers []string, topic, groupID string) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		Topic:       topic,
		GroupID:     groupID,
		MaxWait:     500 * time.Millisecond,
		StartOffset: kafka.FirstOffset,
	})
	return &Consumer{reader: reader}
}

func (c *Consumer) FetchMessage(ctx context.Context) (kafka.Message, error) {
	msg, err := c.reader.FetchMessage(ctx)
	if err != nil {
		return kafka.Message{}, fmt.Errorf("kafka fetch: %w", err)
	}
	return msg, nil
}

func (c *Consumer) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	if err := c.reader.CommitMessages(ctx, msgs...); err != nil {
		return fmt.Errorf("kafka commit: %w", err)
	}
	return nil
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
