//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgadapter "auth-service/internal/adapters/db/postgres"
	"auth-service/internal/domain/entity"
	"auth-service/pkg/testhelpers"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithPostgres(m, "../../../../migrations")
}

func TestUserRepo(t *testing.T) {
	db := testhelpers.MustGetPool(t)
	repo := pgadapter.NewUserRepo(db)

	t.Run("Create and FindByEmail", func(t *testing.T) {
		user := &entity.User{
			ID:           uuid.New(),
			Email:        "user-" + uuid.NewString() + "@test.com",
			PasswordHash: "hashed",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		require.NoError(t, repo.Create(user))

		found, err := repo.FindByEmail(user.Email)
		require.NoError(t, err)
		assert.Equal(t, user.ID, found.ID)
		assert.Equal(t, user.Email, found.Email)
	})

	t.Run("FindByEmail not found returns ErrUserNotFound", func(t *testing.T) {
		_, err := repo.FindByEmail("nobody@nowhere.com")
		assert.ErrorIs(t, err, entity.ErrUserNotFound)
	})

	t.Run("FindByID", func(t *testing.T) {
		user := &entity.User{
			ID:           uuid.New(),
			Email:        "byid-" + uuid.NewString() + "@test.com",
			PasswordHash: "hashed",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		require.NoError(t, repo.Create(user))

		found, err := repo.FindByID(user.ID)
		require.NoError(t, err)
		assert.Equal(t, user.Email, found.Email)
	})

	t.Run("Create duplicate email returns error", func(t *testing.T) {
		email := "dup-" + uuid.NewString() + "@test.com"
		u1 := &entity.User{ID: uuid.New(), Email: email, PasswordHash: "h", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		u2 := &entity.User{ID: uuid.New(), Email: email, PasswordHash: "h", CreatedAt: time.Now(), UpdatedAt: time.Now()}

		require.NoError(t, repo.Create(u1))
		assert.Error(t, repo.Create(u2))
	})
}

func TestSessionRepo(t *testing.T) {
	db := testhelpers.MustGetPool(t)
	userRepo := pgadapter.NewUserRepo(db)
	sessRepo := pgadapter.NewSessionRepo(db)

	newUser := func(t *testing.T) *entity.User {
		t.Helper()
		u := &entity.User{
			ID:           uuid.New(),
			Email:        "sess-" + uuid.NewString() + "@test.com",
			PasswordHash: "hash",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		require.NoError(t, userRepo.Create(u))
		return u
	}

	t.Run("Create and FindByTokenHash", func(t *testing.T) {
		user := newUser(t)
		sess := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: "hash-" + uuid.NewString(),
			UserAgent: "Go-test",
			IPAddress: "127.0.0.1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}

		require.NoError(t, sessRepo.Create(sess))

		found, err := sessRepo.FindByTokenHash(sess.TokenHash)
		require.NoError(t, err)
		assert.Equal(t, sess.ID, found.ID)
		assert.Equal(t, sess.UserID, found.UserID)
		assert.Nil(t, found.RevokedAt)
	})

	t.Run("FindByTokenHash not found", func(t *testing.T) {
		_, err := sessRepo.FindByTokenHash("nonexistent-hash")
		assert.ErrorIs(t, err, entity.ErrSessionNotFound)
	})

	t.Run("RevokeByID sets revoked_at", func(t *testing.T) {
		user := newUser(t)
		sess := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: "revoke-hash-" + uuid.NewString(),
			UserAgent: "test",
			IPAddress: "127.0.0.1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}
		require.NoError(t, sessRepo.Create(sess))
		require.NoError(t, sessRepo.RevokeByID(sess.ID))

		found, err := sessRepo.FindByTokenHash(sess.TokenHash)
		require.NoError(t, err)
		assert.NotNil(t, found.RevokedAt)
	})

	t.Run("RevokeByID already revoked returns ErrSessionNotFound", func(t *testing.T) {
		user := newUser(t)
		sess := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: "dbl-revoke-" + uuid.NewString(),
			UserAgent: "test",
			IPAddress: "127.0.0.1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}
		require.NoError(t, sessRepo.Create(sess))
		require.NoError(t, sessRepo.RevokeByID(sess.ID))

		err := sessRepo.RevokeByID(sess.ID)
		assert.ErrorIs(t, err, entity.ErrSessionNotFound)
	})

	t.Run("RevokeAllByUserID revokes active sessions only", func(t *testing.T) {
		user := newUser(t)
		hash1 := "all-revoke-1-" + uuid.NewString()
		hash2 := "all-revoke-2-" + uuid.NewString()

		for _, h := range []string{hash1, hash2} {
			s := &entity.RefreshSession{
				ID:        uuid.New(),
				UserID:    user.ID,
				TokenHash: h,
				UserAgent: "test",
				IPAddress: "127.0.0.1",
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(time.Hour),
			}
			require.NoError(t, sessRepo.Create(s))
		}

		require.NoError(t, sessRepo.RevokeAllByUserID(user.ID))

		for _, h := range []string{hash1, hash2} {
			found, err := sessRepo.FindByTokenHash(h)
			require.NoError(t, err)
			assert.NotNil(t, found.RevokedAt, "session %s should be revoked", h)
		}
	})

	t.Run("DeleteExpired removes only expired rows", func(t *testing.T) {
		user := newUser(t)

		expired := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: "expired-" + uuid.NewString(),
			UserAgent: "test",
			IPAddress: "127.0.0.1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(-time.Second),
		}
		active := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: "active-" + uuid.NewString(),
			UserAgent: "test",
			IPAddress: "127.0.0.1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}

		require.NoError(t, sessRepo.Create(expired))
		require.NoError(t, sessRepo.Create(active))

		deleted, err := sessRepo.DeleteExpired()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, deleted, int64(1))

		_, err = sessRepo.FindByTokenHash(active.TokenHash)
		assert.NoError(t, err)

		_, err = sessRepo.FindByTokenHash(expired.TokenHash)
		assert.ErrorIs(t, err, entity.ErrSessionNotFound)
	})
}

func init() {
	_ = context.Background()
}
