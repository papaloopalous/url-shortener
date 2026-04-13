package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"shortener-service/internal/domain/entity"
	"shortener-service/internal/usecase"
	"shortener-service/internal/usecase/mocks"

	"github.com/google/uuid"
	gomock "go.uber.org/mock/gomock"
)

func newUsecase(
	ctrl *gomock.Controller,
	urls *mocks.MockURLRepository,
	outbox *mocks.MockOutboxRepository,
	cache *mocks.MockURLCache,
) *usecase.URLUsecase {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return usecase.NewURLUsecase(urls, outbox, cache, "http://short.ly", 90*24*time.Hour, log)
}

func TestCreate_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	outbox.EXPECT().CreateWithURL(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	cache.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	uc := newUsecase(ctrl, urls, outbox, cache)
	out, err := uc.Create(context.Background(), usecase.CreateInput{
		UserID:  uuid.New(),
		LongURL: "https://example.com",
	})

	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(out.ShortCode) != 7 {
		t.Errorf("expected short code length 7, got %d", len(out.ShortCode))
	}
	if out.ShortURL == "" {
		t.Error("ShortURL should not be empty")
	}
}

func TestCreate_CollisionRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	collisionErr := fmt.Errorf("unique constraint: %w", entity.ErrCodeCollision)
	gomock.InOrder(
		outbox.EXPECT().CreateWithURL(gomock.Any(), gomock.Any(), gomock.Any()).Return(collisionErr),
		outbox.EXPECT().CreateWithURL(gomock.Any(), gomock.Any(), gomock.Any()).Return(collisionErr),
		outbox.EXPECT().CreateWithURL(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
	)
	cache.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	uc := newUsecase(ctrl, urls, outbox, cache)
	out, err := uc.Create(context.Background(), usecase.CreateInput{
		UserID:  uuid.New(),
		LongURL: "https://retry.com",
	})

	if err != nil {
		t.Fatalf("Create with retry: %v", err)
	}
	if out.ShortCode == "" {
		t.Error("ShortCode should not be empty after retry")
	}
}

func TestCreate_CacheFailBestEffort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	outbox.EXPECT().CreateWithURL(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	cache.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("redis unavailable"))

	uc := newUsecase(ctrl, urls, outbox, cache)
	out, err := uc.Create(context.Background(), usecase.CreateInput{
		UserID:  uuid.New(),
		LongURL: "https://cache-fail.com",
	})

	if err != nil {
		t.Fatalf("Create should succeed even if cache fails: %v", err)
	}
	if out.ShortCode == "" {
		t.Error("ShortCode should not be empty")
	}
}

func TestRedirect_CacheHit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	expires := time.Now().Add(time.Hour)
	cachedURL := &entity.URL{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		ShortCode: "abc1234",
		LongURL:   "https://example.com",
		Status:    entity.StatusActive,
		ExpiresAt: &expires,
	}
	cache.EXPECT().Get(gomock.Any(), "abc1234").Return(cachedURL, nil)
	urls.EXPECT().FindByCode(gomock.Any(), gomock.Any()).Times(0)
	outbox.EXPECT().AppendEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	uc := newUsecase(ctrl, urls, outbox, cache)
	longURL, err := uc.Redirect(context.Background(), usecase.RedirectInput{Code: "abc1234"})

	if err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	if longURL != "https://example.com" {
		t.Errorf("unexpected long URL: %s", longURL)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestRedirect_CacheMiss_PopulatesCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	expires := time.Now().Add(time.Hour)
	dbURL := &entity.URL{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		ShortCode: "miss123",
		LongURL:   "https://db.com",
		Status:    entity.StatusActive,
		ExpiresAt: &expires,
	}
	cache.EXPECT().Get(gomock.Any(), "miss123").Return(nil, entity.ErrURLNotFound)
	urls.EXPECT().FindByCode(gomock.Any(), "miss123").Return(dbURL, nil)
	cache.EXPECT().Set(gomock.Any(), dbURL, gomock.Any()).Return(nil)
	outbox.EXPECT().AppendEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	uc := newUsecase(ctrl, urls, outbox, cache)
	longURL, err := uc.Redirect(context.Background(), usecase.RedirectInput{Code: "miss123"})

	if err != nil {
		t.Fatalf("Redirect cache miss: %v", err)
	}
	if longURL != "https://db.com" {
		t.Errorf("unexpected long URL: %s", longURL)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestRedirect_ExpiredURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	past := time.Now().Add(-time.Hour)
	expiredURL := &entity.URL{
		ID:        uuid.New(),
		ShortCode: "expired",
		LongURL:   "https://old.com",
		Status:    entity.StatusActive,
		ExpiresAt: &past,
	}
	cache.EXPECT().Get(gomock.Any(), "expired").Return(expiredURL, nil)

	uc := newUsecase(ctrl, urls, outbox, cache)
	_, err := uc.Redirect(context.Background(), usecase.RedirectInput{Code: "expired"})

	if !errors.Is(err, entity.ErrURLExpired) {
		t.Errorf("want ErrURLExpired, got %v", err)
	}
}

func TestRedirect_DeletedURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	now := time.Now()
	deletedURL := &entity.URL{
		ID:        uuid.New(),
		ShortCode: "deleted",
		LongURL:   "https://gone.com",
		Status:    entity.StatusSoftDeleted,
		DeletedAt: &now,
	}
	cache.EXPECT().Get(gomock.Any(), "deleted").Return(deletedURL, nil)

	uc := newUsecase(ctrl, urls, outbox, cache)
	_, err := uc.Redirect(context.Background(), usecase.RedirectInput{Code: "deleted"})

	if !errors.Is(err, entity.ErrURLDeleted) {
		t.Errorf("want ErrURLDeleted, got %v", err)
	}
}

func TestRedirect_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	cache.EXPECT().Get(gomock.Any(), "notexist").Return(nil, entity.ErrURLNotFound)
	urls.EXPECT().FindByCode(gomock.Any(), "notexist").Return(nil, entity.ErrURLNotFound)

	uc := newUsecase(ctrl, urls, outbox, cache)
	_, err := uc.Redirect(context.Background(), usecase.RedirectInput{Code: "notexist"})

	if !errors.Is(err, entity.ErrURLNotFound) {
		t.Errorf("want ErrURLNotFound, got %v", err)
	}
}

func TestBatchDelete_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	ownerID := uuid.New()
	codes := []string{"aaa1111", "bbb2222"}

	urls.EXPECT().SoftDeleteBatch(gomock.Any(), codes, ownerID).Return(int64(2), nil)
	cache.EXPECT().DeleteBatch(gomock.Any(), codes).Return(nil)
	outbox.EXPECT().AppendEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	uc := newUsecase(ctrl, urls, outbox, cache)
	n, err := uc.BatchDelete(context.Background(), usecase.BatchDeleteInput{
		Codes:   codes,
		OwnerID: ownerID,
	})

	if err != nil {
		t.Fatalf("BatchDelete: %v", err)
	}
	if n != 2 {
		t.Errorf("want 2 deleted, got %d", n)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestBatchDelete_NotOwner_ZeroDeleted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urls := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	ownerID := uuid.New()
	codes := []string{"other11"}

	urls.EXPECT().SoftDeleteBatch(gomock.Any(), codes, ownerID).Return(int64(0), nil)
	cache.EXPECT().DeleteBatch(gomock.Any(), codes).Return(nil)
	outbox.EXPECT().AppendEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	uc := newUsecase(ctrl, urls, outbox, cache)
	n, err := uc.BatchDelete(context.Background(), usecase.BatchDeleteInput{
		Codes:   codes,
		OwnerID: ownerID,
	})

	if err != nil {
		t.Fatalf("BatchDelete not owner: %v", err)
	}
	if n != 0 {
		t.Errorf("want 0 deleted (not owner), got %d", n)
	}
	time.Sleep(10 * time.Millisecond)
}
