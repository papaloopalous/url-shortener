package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"shortener-service/internal/domain/entity"
	"shortener-service/internal/outbox"
	"shortener-service/internal/usecase/mocks"

	"github.com/google/uuid"
	gomock "go.uber.org/mock/gomock"
)

func newPoller(ctrl *gomock.Controller, repo *mocks.MockOutboxRepository, pub *mocks.MockPublisher) *outbox.Poller {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return outbox.NewPoller(repo, pub, log, 10, 10*time.Millisecond)
}

func makeEvent(eventType string) *entity.OutboxEvent {
	return &entity.OutboxEvent{
		ID:        uuid.New(),
		EventType: eventType,
		Payload:   []byte(`{"code":"abc1234"}`),
		Status:    entity.OutboxStatusPending,
		CreatedAt: time.Now(),
	}
}

func TestPoller_SuccessfulCycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockOutboxRepository(ctrl)
	pub := mocks.NewMockPublisher(ctrl)

	events := []*entity.OutboxEvent{
		makeEvent(entity.EventURLCreated),
		makeEvent(entity.EventURLClicked),
	}
	ids := []uuid.UUID{events[0].ID, events[1].ID}

	repo.EXPECT().PendingBatch(gomock.Any(), 10).Return(events, nil).Times(1)
	repo.EXPECT().PendingBatch(gomock.Any(), 10).Return(nil, nil).AnyTimes()

	pub.EXPECT().Publish(gomock.Any(), "url-events", gomock.Any(), gomock.Any()).Return(nil)
	pub.EXPECT().Publish(gomock.Any(), "click-events", gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().MarkPublished(gomock.Any(), ids).Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	p := newPoller(ctrl, repo, pub)
	p.Run(ctx)
}

func TestPoller_PartialFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockOutboxRepository(ctrl)
	pub := mocks.NewMockPublisher(ctrl)

	event1 := makeEvent(entity.EventURLCreated)
	event2 := makeEvent(entity.EventURLDeleted)

	repo.EXPECT().PendingBatch(gomock.Any(), 10).Return([]*entity.OutboxEvent{event1, event2}, nil).Times(1)
	repo.EXPECT().PendingBatch(gomock.Any(), 10).Return(nil, nil).AnyTimes()

	pub.EXPECT().Publish(gomock.Any(), "url-events", gomock.Any(), event1.Payload).Return(nil)
	pub.EXPECT().Publish(gomock.Any(), "url-events", gomock.Any(), event2.Payload).Return(errors.New("kafka error"))
	repo.EXPECT().MarkPublished(gomock.Any(), []uuid.UUID{event1.ID}).Return(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	p := newPoller(ctrl, repo, pub)
	p.Run(ctx)
}

func TestPoller_GracefulShutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockOutboxRepository(ctrl)
	pub := mocks.NewMockPublisher(ctrl)

	event := makeEvent(entity.EventURLCreated)
	repo.EXPECT().PendingBatch(gomock.Any(), 10).Return([]*entity.OutboxEvent{event}, nil).AnyTimes()
	pub.EXPECT().Publish(gomock.Any(), "url-events", gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	repo.EXPECT().MarkPublished(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())

	p := newPoller(ctrl, repo, pub)

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("poller did not shut down in time")
	}
}

func TestPoller_EmptyBatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockOutboxRepository(ctrl)
	pub := mocks.NewMockPublisher(ctrl)

	repo.EXPECT().PendingBatch(gomock.Any(), 10).Return(nil, nil).AnyTimes()
	pub.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	repo.EXPECT().MarkPublished(gomock.Any(), gomock.Any()).Times(0)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	p := newPoller(ctrl, repo, pub)
	p.Run(ctx)
}
