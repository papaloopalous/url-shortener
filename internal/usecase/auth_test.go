package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	"auth-service/internal/domain/entity"
	"auth-service/internal/usecase"
	"auth-service/internal/usecase/mocks"
)

var ctx = context.Background()

func setup(t *testing.T) (
	*usecase.AuthUsecase,
	*mocks.MockUserRepository,
	*mocks.MockSessionRepository,
	*mocks.MockTokenCache,
	*mocks.MockTokenManager,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	users := mocks.NewMockUserRepository(ctrl)
	sessions := mocks.NewMockSessionRepository(ctrl)
	cache := mocks.NewMockTokenCache(ctrl)
	tokens := mocks.NewMockTokenManager(ctrl)
	uc := usecase.NewAuthUsecase(
		users, sessions, cache, tokens,
		15*time.Minute, 7*24*time.Hour, bcrypt.MinCost,
	)
	return uc, users, sessions, cache, tokens
}

func mustHash(t *testing.T, raw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

func TestRegister(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc, users, sessions, _, tokens := setup(t)

		users.EXPECT().FindByEmail(gomock.Any(), "alice@example.com").Return(nil, entity.ErrUserNotFound)
		users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		tokens.EXPECT().GenerateAccess(gomock.Any(), 15*time.Minute).Return("access.tok", "jti-1", nil)
		sessions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		pair, err := uc.Register(ctx,
			usecase.RegisterInput{Email: "alice@example.com", Password: "Str0ngPass!"},
			"agent", "1.1.1.1",
		)
		require.NoError(t, err)
		assert.Equal(t, "access.tok", pair.AccessToken)
		assert.NotEmpty(t, pair.RefreshToken)
	})

	t.Run("duplicate email", func(t *testing.T) {
		uc, users, _, _, _ := setup(t)

		users.EXPECT().FindByEmail(gomock.Any(), "alice@example.com").
			Return(&entity.User{ID: uuid.New()}, nil)

		_, err := uc.Register(ctx,
			usecase.RegisterInput{Email: "alice@example.com", Password: "whatever"},
			"", "",
		)
		assert.ErrorIs(t, err, entity.ErrUserAlreadyExists)
	})
}

func TestLogin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc, users, sessions, _, tokens := setup(t)

		user := &entity.User{ID: uuid.New(), PasswordHash: mustHash(t, "correct")}
		users.EXPECT().FindByEmail(gomock.Any(), "bob@example.com").Return(user, nil)
		tokens.EXPECT().GenerateAccess(user.ID.String(), 15*time.Minute).Return("tok", "jti", nil)
		sessions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		pair, err := uc.Login(ctx,
			usecase.LoginInput{Email: "bob@example.com", Password: "correct"},
			"", "",
		)
		require.NoError(t, err)
		assert.Equal(t, "tok", pair.AccessToken)
	})

	t.Run("wrong password", func(t *testing.T) {
		uc, users, _, _, _ := setup(t)

		user := &entity.User{PasswordHash: mustHash(t, "real")}
		users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).Return(user, nil)

		_, err := uc.Login(ctx, usecase.LoginInput{Email: "x@x.com", Password: "wrong"}, "", "")
		assert.ErrorIs(t, err, entity.ErrInvalidPassword)
	})

	t.Run("user not found returns generic error (no enumeration)", func(t *testing.T) {
		uc, users, _, _, _ := setup(t)

		users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).Return(nil, entity.ErrUserNotFound)

		_, err := uc.Login(ctx, usecase.LoginInput{Email: "ghost@x.com", Password: "pw"}, "", "")

		assert.ErrorIs(t, err, entity.ErrInvalidPassword)
		assert.False(t, errors.Is(err, entity.ErrUserNotFound))
	})
}

func TestRefresh(t *testing.T) {
	t.Run("reuse detection revokes all sessions", func(t *testing.T) {
		uc, _, sessions, _, _ := setup(t)

		revokedAt := time.Now().Add(-time.Hour)
		userID := uuid.New()
		session := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    userID,
			ExpiresAt: time.Now().Add(24 * time.Hour),
			RevokedAt: &revokedAt,
		}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)
		sessions.EXPECT().RevokeAllByUserID(gomock.Any(), userID).Return(nil)

		_, err := uc.Refresh(ctx, "stale-token", "", "")
		assert.ErrorIs(t, err, entity.ErrTokenReuse)
	})

	t.Run("expired session", func(t *testing.T) {
		uc, _, sessions, _, _ := setup(t)

		session := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)

		_, err := uc.Refresh(ctx, "expired-token", "", "")
		assert.ErrorIs(t, err, entity.ErrSessionExpired)
	})

	t.Run("success rotates token", func(t *testing.T) {
		uc, _, sessions, _, tokens := setup(t)

		userID := uuid.New()
		session := &entity.RefreshSession{
			ID:        uuid.New(),
			UserID:    userID,
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)
		sessions.EXPECT().RevokeByID(gomock.Any(), session.ID).Return(nil)
		tokens.EXPECT().GenerateAccess(userID.String(), 15*time.Minute).Return("new.tok", "new-jti", nil)
		sessions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		pair, err := uc.Refresh(ctx, "valid-raw-token", "", "")
		require.NoError(t, err)
		assert.Equal(t, "new.tok", pair.AccessToken)
		assert.NotEmpty(t, pair.RefreshToken)
	})
}

func TestLogout(t *testing.T) {
	t.Run("revokes session and blacklists jti", func(t *testing.T) {
		uc, _, sessions, cache, _ := setup(t)

		session := &entity.RefreshSession{
			ID:        uuid.New(),
			ExpiresAt: time.Now().Add(time.Hour),
		}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)
		sessions.EXPECT().RevokeByID(gomock.Any(), session.ID).Return(nil)
		cache.EXPECT().RevokeJTI(gomock.Any(), "jti-abc", 5*time.Minute).Return(nil)

		err := uc.Logout(ctx, "raw-token", "jti-abc", 5*time.Minute)
		assert.NoError(t, err)
	})

	t.Run("session not found still blacklists jti", func(t *testing.T) {
		uc, _, sessions, cache, _ := setup(t)

		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(nil, entity.ErrSessionNotFound)
		cache.EXPECT().RevokeJTI(gomock.Any(), "jti-xyz", 10*time.Minute).Return(nil)

		err := uc.Logout(ctx, "any-token", "jti-xyz", 10*time.Minute)
		assert.NoError(t, err)
	})
}
