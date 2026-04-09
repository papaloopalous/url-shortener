//go:build integration

package redis_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	redisadapter "auth-service/internal/adapters/cache/redis"
	"auth-service/pkg/testhelpers"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithRedis(m)
}

func TestTokenCacheAdapter(t *testing.T) {
	rdb := testhelpers.MustGetRedis(t)
	cache := redisadapter.NewTokenCacheAdapter(rdb)

	t.Run("RevokeJTI and IsRevoked returns true", func(t *testing.T) {
		jti := "test-jti-revoked"

		require.NoError(t, cache.RevokeJTI(jti, time.Minute))

		revoked, err := cache.IsRevoked(jti)
		require.NoError(t, err)
		assert.True(t, revoked)
	})

	t.Run("IsRevoked returns false for unknown jti", func(t *testing.T) {
		revoked, err := cache.IsRevoked("not-revoked-jti")
		require.NoError(t, err)
		assert.False(t, revoked)
	})

	t.Run("key expires after TTL", func(t *testing.T) {
		jti := "test-jti-expires"

		require.NoError(t, cache.RevokeJTI(jti, 100*time.Millisecond))

		revoked, err := cache.IsRevoked(jti)
		require.NoError(t, err)
		assert.True(t, revoked)

		time.Sleep(200 * time.Millisecond)

		revoked, err = cache.IsRevoked(jti)
		require.NoError(t, err)
		assert.False(t, revoked)
	})

	t.Run("different jti values are independent", func(t *testing.T) {
		require.NoError(t, cache.RevokeJTI("jti-a", time.Minute))

		revokedA, err := cache.IsRevoked("jti-a")
		require.NoError(t, err)
		assert.True(t, revokedA)

		revokedB, err := cache.IsRevoked("jti-b")
		require.NoError(t, err)
		assert.False(t, revokedB)
	})
}
