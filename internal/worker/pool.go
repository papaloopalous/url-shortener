package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"analytics-service/internal/domain/entity"
	"analytics-service/internal/domain/service"
	"analytics-service/pkg/metrics"
	"analytics-service/pkg/tracing"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
)

type clickPayload struct {
	ShortCode string `json:"short_code"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Referer   string `json:"referer"`
	Country   string `json:"country"`
	TS        int64  `json:"ts"`
}

type Consumer interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type Pool struct {
	consumer    Consumer
	inboxRepo   service.InboxRepository
	statsCache  service.StatsCache
	log         *slog.Logger
	concurrency int
}

func NewPool(
	consumer Consumer,
	inboxRepo service.InboxRepository,
	statsCache service.StatsCache,
	log *slog.Logger,
	concurrency int,
) *Pool {
	return &Pool{
		consumer:    consumer,
		inboxRepo:   inboxRepo,
		statsCache:  statsCache,
		log:         log,
		concurrency: concurrency,
	}
}

func (p *Pool) Run(ctx context.Context) {
	p.log.InfoContext(ctx, "worker pool started", "concurrency", p.concurrency)

	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup

	for {
		msg, err := p.consumer.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			p.log.ErrorContext(ctx, "fetch kafka message", "error", err)
			continue
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break
		}

		metrics.WorkerActive(1)
		wg.Add(1)
		go func(m kafka.Message) {
			defer func() {
				<-sem
				metrics.WorkerActive(-1)
				wg.Done()
			}()

			p.process(ctx, m)

			if commitErr := p.consumer.CommitMessages(ctx, m); commitErr != nil {
				if !errors.Is(commitErr, context.Canceled) {
					p.log.ErrorContext(ctx, "commit kafka message", "error", commitErr)
				}
			}
		}(msg)
	}

	wg.Wait()
	p.log.InfoContext(ctx, "worker pool stopped")
}

func (p *Pool) process(ctx context.Context, msg kafka.Message) {
	tracer := tracing.Tracer("worker/pool")
	ctx, span := tracer.Start(ctx, "Pool.process")
	defer span.End()

	var payload clickPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		p.log.ErrorContext(ctx, "unmarshal click payload", "error", err, "offset", msg.Offset)
		metrics.IncEvent(metrics.EventClickFailed)
		return
	}

	span.SetAttributes(attribute.String("click.short_code", payload.ShortCode))

	eventID, err := uuid.ParseBytes(msg.Key)
	if err != nil {
		eventID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("%d-%d", msg.Partition, msg.Offset)))
	}

	clickedAt := time.Unix(payload.TS, 0)
	if payload.TS == 0 {
		clickedAt = msg.Time
	}

	click := &entity.ClickEvent{
		ID:        uuid.New(),
		ShortCode: payload.ShortCode,
		IP:        payload.IP,
		UserAgent: payload.UserAgent,
		Referer:   payload.Referer,
		Country:   payload.Country,
		ClickedAt: clickedAt,
	}

	if saveErr := p.inboxRepo.SaveWithClick(ctx, eventID, click); saveErr != nil {
		if errors.Is(saveErr, entity.ErrDuplicateEvent) {
			p.log.DebugContext(ctx, "duplicate event, skipping",
				"event_id", eventID, "short_code", payload.ShortCode)
			metrics.IncEvent(metrics.EventClickDuplicate)
			return
		}
		p.log.ErrorContext(ctx, "save click event",
			"error", saveErr, "event_id", eventID, "short_code", payload.ShortCode)
		metrics.IncEvent(metrics.EventClickFailed)
		return
	}

	if err := p.statsCache.Invalidate(ctx, payload.ShortCode); err != nil {
		p.log.WarnContext(ctx, "invalidate stats cache (best-effort)",
			"error", err, "short_code", payload.ShortCode)
	}

	metrics.IncEvent(metrics.EventClickProcessed)
}
