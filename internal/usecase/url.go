package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"shortener-service/internal/domain/entity"
	"shortener-service/internal/domain/service"
	"shortener-service/pkg/metrics"
	"shortener-service/pkg/tracing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

const (
	alphabet   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLen    = 7
	maxRetries = 3
	cacheTTL   = 24 * time.Hour
)

type URLUsecase struct {
	urls       service.URLRepository
	outbox     service.OutboxRepository
	cache      service.URLCache
	baseURL    string
	defaultTTL time.Duration
	log        *slog.Logger
}

func NewURLUsecase(
	urls service.URLRepository,
	outbox service.OutboxRepository,
	cache service.URLCache,
	baseURL string,
	defaultTTL time.Duration,
	log *slog.Logger,
) *URLUsecase {
	return &URLUsecase{
		urls:       urls,
		outbox:     outbox,
		cache:      cache,
		baseURL:    baseURL,
		defaultTTL: defaultTTL,
		log:        log,
	}
}

func (uc *URLUsecase) Create(ctx context.Context, in CreateInput) (CreateOutput, error) {
	tracer := tracing.Tracer("usecase/url")
	ctx, span := tracer.Start(ctx, "URLUsecase.Create")
	defer span.End()

	span.SetAttributes(
		attribute.String("url.user_id", in.UserID.String()),
		attribute.String("url.long_url", in.LongURL),
	)

	ttl := uc.defaultTTL
	if in.TTL != nil {
		ttl = *in.TTL
	}

	expiresAt := time.Now().Add(ttl)
	now := time.Now()

	var url *entity.URL
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		code := generateCode()
		url = &entity.URL{
			ID:        uuid.New(),
			UserID:    in.UserID,
			ShortCode: code,
			LongURL:   in.LongURL,
			Status:    entity.StatusActive,
			ExpiresAt: &expiresAt,
			CreatedAt: now,
			UpdatedAt: now,
		}

		payload, _ := json.Marshal(map[string]any{
			"short_code": code,
			"long_url":   in.LongURL,
			"user_id":    in.UserID.String(),
		})

		event := &entity.OutboxEvent{
			ID:        uuid.New(),
			EventType: entity.EventURLCreated,
			Payload:   payload,
			Status:    entity.OutboxStatusPending,
			CreatedAt: now,
		}

		err = uc.outbox.CreateWithURL(ctx, url, event)
		if err == nil {
			break
		}
		uc.log.WarnContext(ctx, "short code collision, retrying",
			"attempt", attempt+1, "code", code)
	}
	if err != nil {
		return CreateOutput{}, fmt.Errorf("create url: %w", err)
	}

	span.SetAttributes(attribute.String("url.short_code", url.ShortCode))

	cacheTTLVal := cacheTTL
	if url.ExpiresAt != nil {
		remaining := time.Until(*url.ExpiresAt)
		if remaining < cacheTTLVal {
			cacheTTLVal = remaining
		}
	}
	if err := uc.cache.Set(ctx, url, cacheTTLVal); err != nil {
		uc.log.WarnContext(ctx, "cache set failed (best-effort)", "error", err)
	}

	metrics.EventsTotal.WithLabelValues(metrics.EventURLCreated).Inc()

	return CreateOutput{
		ShortCode: url.ShortCode,
		ShortURL:  uc.baseURL + "/" + url.ShortCode,
		LongURL:   url.LongURL,
		ExpiresAt: *url.ExpiresAt,
	}, nil
}

func (uc *URLUsecase) Redirect(ctx context.Context, in RedirectInput) (string, error) {
	tracer := tracing.Tracer("usecase/url")
	ctx, span := tracer.Start(ctx, "URLUsecase.Redirect")
	defer span.End()

	span.SetAttributes(attribute.String("url.short_code", in.Code))

	url, err := uc.cache.Get(ctx, in.Code)
	if err == nil {
		span.SetAttributes(attribute.Bool("url.cache_hit", true))
		metrics.EventsTotal.WithLabelValues(metrics.EventCacheHit).Inc()
	} else {
		span.SetAttributes(attribute.Bool("url.cache_hit", false))
		metrics.EventsTotal.WithLabelValues(metrics.EventCacheMiss).Inc()

		url, err = uc.urls.FindByCode(ctx, in.Code)
		if err != nil {
			return "", fmt.Errorf("redirect find by code: %w", err)
		}

		cacheTTLVal := cacheTTL
		if url.ExpiresAt != nil {
			if remaining := time.Until(*url.ExpiresAt); remaining < cacheTTLVal {
				cacheTTLVal = remaining
			}
		}
		if cacheErr := uc.cache.Set(ctx, url, cacheTTLVal); cacheErr != nil {
			uc.log.WarnContext(ctx, "cache set failed on miss (best-effort)", "error", cacheErr)
		}
	}

	if err := url.IsAccessible(); err != nil {
		return "", err
	}

	go func() {
		payload, _ := json.Marshal(map[string]any{
			"short_code": in.Code,
			"ip":         in.IP,
			"user_agent": in.UserAgent,
			"referer":    in.Referer,
			"ts":         time.Now().Unix(),
		})
		event := &entity.OutboxEvent{
			ID:        uuid.New(),
			EventType: entity.EventURLClicked,
			Payload:   payload,
			Status:    entity.OutboxStatusPending,
			CreatedAt: time.Now(),
		}
		// используем Background т.к. горутина живёт дольше запроса
		if appendErr := uc.outbox.AppendEvent(context.Background(), event); appendErr != nil {
			uc.log.Warn("append click event failed (best-effort)", "error", appendErr)
		}
	}()

	metrics.EventsTotal.WithLabelValues(metrics.EventURLRedirected).Inc()
	return url.LongURL, nil
}

func (uc *URLUsecase) ListByUser(ctx context.Context, userID uuid.UUID) ([]*entity.URL, error) {
	tracer := tracing.Tracer("usecase/url")
	ctx, span := tracer.Start(ctx, "URLUsecase.ListByUser")
	defer span.End()

	span.SetAttributes(attribute.String("url.user_id", userID.String()))

	urls, err := uc.urls.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list by user: %w", err)
	}
	return urls, nil
}

func (uc *URLUsecase) BatchDelete(ctx context.Context, in BatchDeleteInput) (int64, error) {
	tracer := tracing.Tracer("usecase/url")
	ctx, span := tracer.Start(ctx, "URLUsecase.BatchDelete")
	defer span.End()

	span.SetAttributes(
		attribute.String("url.user_id", in.OwnerID.String()),
		attribute.Int("url.codes_count", len(in.Codes)),
	)

	n, err := uc.urls.SoftDeleteBatch(ctx, in.Codes, in.OwnerID)
	if err != nil {
		return 0, fmt.Errorf("batch delete: %w", err)
	}

	if cacheErr := uc.cache.DeleteBatch(ctx, in.Codes); cacheErr != nil {
		uc.log.WarnContext(ctx, "cache delete batch failed (best-effort)", "error", cacheErr)
	}

	go func() {
		payload, _ := json.Marshal(map[string]any{
			"codes":    in.Codes,
			"owner_id": in.OwnerID.String(),
			"ts":       time.Now().Unix(),
		})
		event := &entity.OutboxEvent{
			ID:        uuid.New(),
			EventType: entity.EventURLDeleted,
			Payload:   payload,
			Status:    entity.OutboxStatusPending,
			CreatedAt: time.Now(),
		}
		if appendErr := uc.outbox.AppendEvent(context.Background(), event); appendErr != nil {
			uc.log.Warn("append delete event failed (best-effort)", "error", appendErr)
		}
	}()

	metrics.EventsTotal.WithLabelValues(metrics.EventURLDeleted).Inc()
	return n, nil
}

func generateCode() string {
	b := make([]byte, codeLen)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(b)
}
