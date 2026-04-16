package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"analytics-service/internal/domain/entity"
	"analytics-service/internal/usecase/mocks"
	"analytics-service/internal/worker"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	gomock "go.uber.org/mock/gomock"
)

// fakeConsumer заглушка Kafka Consumer для unit тестов
type fakeConsumer struct {
	msgs   chan kafka.Message
	closed bool
}

func newFakeConsumer(msgs ...kafka.Message) *fakeConsumer {
	ch := make(chan kafka.Message, len(msgs))
	for _, m := range msgs {
		ch <- m
	}
	return &fakeConsumer{msgs: ch}
}

func (f *fakeConsumer) FetchMessage(ctx context.Context) (kafka.Message, error) {
	select {
	case <-ctx.Done():
		return kafka.Message{}, context.Canceled
	case msg, ok := <-f.msgs:
		if !ok {
			return kafka.Message{}, context.Canceled
		}
		return msg, nil
	}
}

func (f *fakeConsumer) CommitMessages(_ context.Context, _ ...kafka.Message) error {
	return nil
}

func (f *fakeConsumer) Close() error {
	if !f.closed {
		f.closed = true
		close(f.msgs)
	}
	return nil
}

func makeMsg(shortCode string, eventID uuid.UUID) kafka.Message {
	payload, _ := json.Marshal(map[string]any{
		"short_code": shortCode,
		"ip":         "1.2.3.4",
		"user_agent": "Mozilla/5.0",
		"referer":    "",
		"country":    "RU",
		"ts":         time.Now().Unix(),
	})
	return kafka.Message{
		Key:   []byte(eventID.String()),
		Value: payload,
		Time:  time.Now(),
	}
}

func newPool(
	ctrl *gomock.Controller,
	consumer worker.Consumer,
	inboxRepo *mocks.MockInboxRepository,
	statsCache *mocks.MockStatsCache,
) *worker.Pool {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return worker.NewPool(consumer, inboxRepo, statsCache, log, 2)
}

func TestPool_SuccessfulProcessing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	inboxRepo := mocks.NewMockInboxRepository(ctrl)
	statsCache := mocks.NewMockStatsCache(ctrl)

	eventID := uuid.New()
	msg := makeMsg("abc1234", eventID)
	consumer := newFakeConsumer(msg)

	inboxRepo.EXPECT().
		SaveWithClick(gomock.Any(), eventID, gomock.Any()).
		Return(nil)
	statsCache.EXPECT().
		Invalidate(gomock.Any(), "abc1234").
		Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool := newPool(ctrl, consumer, inboxRepo, statsCache)
	pool.Run(ctx)
}

func TestPool_DuplicateEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	inboxRepo := mocks.NewMockInboxRepository(ctrl)
	statsCache := mocks.NewMockStatsCache(ctrl)

	eventID := uuid.New()
	msg := makeMsg("dup1234", eventID)
	consumer := newFakeConsumer(msg)

	inboxRepo.EXPECT().
		SaveWithClick(gomock.Any(), eventID, gomock.Any()).
		Return(entity.ErrDuplicateEvent)
	statsCache.EXPECT().Invalidate(gomock.Any(), gomock.Any()).Times(0)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool := newPool(ctrl, consumer, inboxRepo, statsCache)
	pool.Run(ctx)
}

func TestPool_SaveError_NoCommit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	inboxRepo := mocks.NewMockInboxRepository(ctrl)
	statsCache := mocks.NewMockStatsCache(ctrl)

	eventID := uuid.New()
	msg := makeMsg("err1234", eventID)
	consumer := newFakeConsumer(msg)

	inboxRepo.EXPECT().
		SaveWithClick(gomock.Any(), eventID, gomock.Any()).
		Return(errors.New("postgres unavailable"))
	statsCache.EXPECT().Invalidate(gomock.Any(), gomock.Any()).Times(0)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool := newPool(ctrl, consumer, inboxRepo, statsCache)
	pool.Run(ctx)
}

func TestPool_InvalidPayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	inboxRepo := mocks.NewMockInboxRepository(ctrl)
	statsCache := mocks.NewMockStatsCache(ctrl)

	msg := kafka.Message{
		Key:   []byte(uuid.New().String()),
		Value: []byte(`not json`),
		Time:  time.Now(),
	}
	consumer := newFakeConsumer(msg)

	inboxRepo.EXPECT().SaveWithClick(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	statsCache.EXPECT().Invalidate(gomock.Any(), gomock.Any()).Times(0)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool := newPool(ctrl, consumer, inboxRepo, statsCache)
	pool.Run(ctx)
}

func TestPool_GracefulShutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	inboxRepo := mocks.NewMockInboxRepository(ctrl)
	statsCache := mocks.NewMockStatsCache(ctrl)

	infiniteConsumer := &blockingConsumer{}

	inboxRepo.EXPECT().SaveWithClick(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()
	statsCache.EXPECT().Invalidate(gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	pool := worker.NewPool(infiniteConsumer, inboxRepo, statsCache, log, 2)

	done := make(chan struct{})
	go func() {
		pool.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("pool did not shut down in time")
	}
}

type blockingConsumer struct{}

func (b *blockingConsumer) FetchMessage(ctx context.Context) (kafka.Message, error) {
	<-ctx.Done()
	return kafka.Message{}, context.Canceled
}

func (b *blockingConsumer) CommitMessages(_ context.Context, _ ...kafka.Message) error {
	return nil
}

func (b *blockingConsumer) Close() error { return nil }
