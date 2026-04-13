//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"shortener-service/internal/adapters/db/postgres"
	"shortener-service/internal/domain/entity"
	"shortener-service/pkg/testhelpers"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithPostgres(m, "../../../../migrations")
}

func TestURLRepo_Create_FindByCode(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	repo := postgres.NewURLRepo(pool)
	ctx := context.Background()

	url := &entity.URL{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		ShortCode: "abc1234",
		LongURL:   "https://example.com",
		Status:    entity.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := repo.Create(ctx, url); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByCode(ctx, url.ShortCode)
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if got.LongURL != url.LongURL {
		t.Errorf("LongURL mismatch: want %s, got %s", url.LongURL, got.LongURL)
	}
}

func TestURLRepo_FindByCode_NotFound(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	repo := postgres.NewURLRepo(pool)
	ctx := context.Background()

	_, err := repo.FindByCode(ctx, "notexist")
	if !errors.Is(err, entity.ErrURLNotFound) {
		t.Errorf("want ErrURLNotFound, got %v", err)
	}
}

func TestURLRepo_SoftDeleteBatch_OwnerCheck(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	repo := postgres.NewURLRepo(pool)
	ctx := context.Background()

	ownerID := uuid.New()
	otherID := uuid.New()

	url1 := &entity.URL{
		ID: uuid.New(), UserID: ownerID, ShortCode: "aaa1111",
		LongURL: "https://a.com", Status: entity.StatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	url2 := &entity.URL{
		ID: uuid.New(), UserID: otherID, ShortCode: "bbb2222",
		LongURL: "https://b.com", Status: entity.StatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	if err := repo.Create(ctx, url1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, url2); err != nil {
		t.Fatal(err)
	}

	// Пытаемся удалить оба кода от имени ownerID — должен удалиться только url1.
	n, err := repo.SoftDeleteBatch(ctx, []string{"aaa1111", "bbb2222"}, ownerID)
	if err != nil {
		t.Fatalf("SoftDeleteBatch: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 deleted, got %d", n)
	}

	got, err := repo.FindByCode(ctx, "aaa1111")
	if err != nil {
		t.Fatal(err)
	}
	if got.DeletedAt == nil {
		t.Error("url1 should have deleted_at set")
	}
	if got.Status != entity.StatusSoftDeleted {
		t.Errorf("url1 status should be soft_deleted, got %s", got.Status)
	}
}

func TestOutboxRepo_CreateWithURL_Atomic(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	outboxRepo := postgres.NewOutboxRepo(pool)
	urlRepo := postgres.NewURLRepo(pool)
	ctx := context.Background()

	url := &entity.URL{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		ShortCode: "atomic1",
		LongURL:   "https://atomic.com",
		Status:    entity.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	event := &entity.OutboxEvent{
		ID:        uuid.New(),
		EventType: entity.EventURLCreated,
		Payload:   []byte(`{"code":"atomic1"}`),
		Status:    entity.OutboxStatusPending,
		CreatedAt: time.Now(),
	}

	if err := outboxRepo.CreateWithURL(ctx, url, event); err != nil {
		t.Fatalf("CreateWithURL: %v", err)
	}

	got, err := urlRepo.FindByCode(ctx, "atomic1")
	if err != nil {
		t.Fatalf("FindByCode after CreateWithURL: %v", err)
	}
	if got.ShortCode != "atomic1" {
		t.Error("url not found after atomic insert")
	}
}

func TestOutboxRepo_PendingBatch_SkipLocked(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	outboxRepo := postgres.NewOutboxRepo(pool)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		e := &entity.OutboxEvent{
			ID:        uuid.New(),
			EventType: entity.EventURLCreated,
			Payload:   []byte(`{}`),
			Status:    entity.OutboxStatusPending,
			CreatedAt: time.Now(),
		}
		if err := outboxRepo.AppendEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	total := 0
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events, err := outboxRepo.PendingBatch(ctx, 3)
			if err != nil {
				return
			}
			mu.Lock()
			total += len(events)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if total == 0 {
		t.Error("expected some events, got 0")
	}
}

func TestOutboxRepo_MarkPublished(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	outboxRepo := postgres.NewOutboxRepo(pool)
	ctx := context.Background()

	eventID := uuid.New()
	e := &entity.OutboxEvent{
		ID:        eventID,
		EventType: entity.EventURLCreated,
		Payload:   []byte(`{}`),
		Status:    entity.OutboxStatusPending,
		CreatedAt: time.Now(),
	}
	if err := outboxRepo.AppendEvent(ctx, e); err != nil {
		t.Fatal(err)
	}

	if err := outboxRepo.MarkPublished(ctx, []uuid.UUID{eventID}); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	pending, err := outboxRepo.PendingBatch(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range pending {
		if ev.ID == eventID {
			t.Error("published event should not appear in pending batch")
		}
	}
}
