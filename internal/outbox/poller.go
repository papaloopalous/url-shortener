package outbox

import (
	"context"
	"log/slog"
	"time"

	"shortener-service/internal/domain/entity"
	"shortener-service/internal/domain/service"
	"shortener-service/pkg/metrics"
	"shortener-service/pkg/tracing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

const (
	topicURLEvents   = "url-events"
	topicClickEvents = "click-events"
)

type Poller struct {
	outbox    service.OutboxRepository
	publisher service.Publisher
	log       *slog.Logger
	batchSize int
	interval  time.Duration
}

func NewPoller(
	outbox service.OutboxRepository,
	publisher service.Publisher,
	log *slog.Logger,
	batchSize int,
	interval time.Duration,
) *Poller {
	return &Poller{
		outbox:    outbox,
		publisher: publisher,
		log:       log,
		batchSize: batchSize,
		interval:  interval,
	}
}

func (p *Poller) Run(ctx context.Context) {
	p.log.InfoContext(ctx, "outbox poller started",
		"batch_size", p.batchSize,
		"interval", p.interval,
	)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.InfoContext(ctx, "outbox poller shutting down")
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.log.ErrorContext(ctx, "outbox poll failed", "error", err)
			}
		}
	}
}

func (p *Poller) poll(ctx context.Context) error {
	tracer := tracing.Tracer("outbox/poller")
	ctx, span := tracer.Start(ctx, "Poller.poll")
	defer span.End()

	events, err := p.outbox.PendingBatch(ctx, p.batchSize)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		return nil
	}

	span.SetAttributes(attribute.Int("outbox.batch_size", len(events)))
	metrics.OutboxPendingTotal.Set(float64(len(events)))

	var publishedIDs []uuid.UUID

	for _, event := range events {
		topic := topicForEvent(event.EventType)
		key := keyForEvent(event)

		if pubErr := p.publisher.Publish(ctx, topic, key, event.Payload); pubErr != nil {
			p.log.WarnContext(ctx, "publish failed, will retry",
				"event_id", event.ID,
				"event_type", event.EventType,
				"error", pubErr,
			)
			metrics.EventsTotal.WithLabelValues(metrics.EventOutboxFailed).Inc()
			continue
		}

		publishedIDs = append(publishedIDs, event.ID)
		metrics.EventsTotal.WithLabelValues(metrics.EventOutboxPublished).Inc()
	}

	span.SetAttributes(attribute.Int("outbox.published", len(publishedIDs)))

	if len(publishedIDs) > 0 {
		if err := p.outbox.MarkPublished(ctx, publishedIDs); err != nil {
			p.log.ErrorContext(ctx, "mark published failed", "error", err)
			return err
		}
	}

	p.log.InfoContext(ctx, "outbox poll complete",
		"total", len(events),
		"published", len(publishedIDs),
		"failed", len(events)-len(publishedIDs),
	)
	return nil
}

func topicForEvent(eventType string) string {
	if eventType == entity.EventURLClicked {
		return topicClickEvents
	}
	return topicURLEvents
}

func keyForEvent(event *entity.OutboxEvent) []byte {
	return []byte(event.ID.String())
}
