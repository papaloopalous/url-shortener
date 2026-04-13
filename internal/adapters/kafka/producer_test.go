//go:build integration

package kafka_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"shortener-service/internal/adapters/kafka"

	segkafka "github.com/segmentio/kafka-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
)

func TestProducer_Publish(t *testing.T) {
	ctx := context.Background()

	var container *tckafka.KafkaContainer
	var err error

	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "integration: testcontainers panic: %v, skipping\n", r)
				t.Skip("Docker not available, skipping kafka integration test")
			}
		}()
		container, err = tckafka.Run(ctx, "confluentinc/cp-kafka:7.6.0")
	}()

	if err != nil {
		t.Skipf("start kafka container: %v, skipping", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("get brokers: %v", err)
	}

	topic := "test-topic"
	producer := kafka.NewProducer(brokers)
	t.Cleanup(func() { _ = producer.Close() })

	key := []byte("test-key")
	value := []byte(`{"event":"url.created","code":"abc1234"}`)

	if err := producer.Publish(ctx, topic, key, value); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	reader := segkafka.NewReader(segkafka.ReaderConfig{
		Brokers:   brokers,
		Topic:     topic,
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  1e6,
		MaxWait:   2 * time.Second,
	})
	t.Cleanup(func() { _ = reader.Close() })

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msg, err := reader.ReadMessage(readCtx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if string(msg.Key) != string(key) {
		t.Errorf("key mismatch: want %s, got %s", key, msg.Key)
	}
	if string(msg.Value) != string(value) {
		t.Errorf("value mismatch: want %s, got %s", value, msg.Value)
	}
}
