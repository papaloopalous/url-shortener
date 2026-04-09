package grpc_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	grpcctrl "auth-service/internal/controller/grpc"
	"auth-service/internal/domain/entity"
	"auth-service/internal/usecase"
	"auth-service/internal/usecase/mocks"
	pb "auth-service/proto/auth"
)

const bufSize = 1024 * 1024

func startServer(t *testing.T, tokenMgr *mocks.MockTokenManager, tokenCache *mocks.MockTokenCache) pb.AuthServiceClient {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	pb.RegisterAuthServiceServer(srv, grpcctrl.NewAuthGRPCServer(tokenMgr, tokenCache, log))

	go func() {
		if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			t.Logf("grpc server error: %v", err)
		}
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewAuthServiceClient(conn)
}

func setupGRPC(t *testing.T) (pb.AuthServiceClient, *mocks.MockTokenManager, *mocks.MockTokenCache) {
	t.Helper()
	ctrl := gomock.NewController(t)
	tokenMgr := mocks.NewMockTokenManager(ctrl)
	tokenCache := mocks.NewMockTokenCache(ctrl)
	return startServer(t, tokenMgr, tokenCache), tokenMgr, tokenCache
}

func TestValidateToken(t *testing.T) {
	t.Run("valid token returns user_id and jti", func(t *testing.T) {
		client, tokenMgr, tokenCache := setupGRPC(t)

		tokenMgr.EXPECT().
			VerifyAccess("valid.token").
			Return("user-123", "jti-abc", nil)

		tokenCache.EXPECT().
			IsRevoked(gomock.Any(), "jti-abc").
			Return(false, nil)

		resp, err := client.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
			AccessToken: "valid.token",
		})

		require.NoError(t, err)
		assert.Equal(t, "user-123", resp.UserId)
		assert.Equal(t, "jti-abc", resp.Jti)
	})

	t.Run("invalid signature returns UNAUTHENTICATED", func(t *testing.T) {
		client, tokenMgr, _ := setupGRPC(t)

		tokenMgr.EXPECT().
			VerifyAccess("bad.token").
			Return("", "", entity.ErrInvalidPassword)

		_, err := client.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
			AccessToken: "bad.token",
		})

		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("revoked jti returns UNAUTHENTICATED", func(t *testing.T) {
		client, tokenMgr, tokenCache := setupGRPC(t)

		tokenMgr.EXPECT().
			VerifyAccess("revoked.token").
			Return("user-999", "jti-revoked", nil)

		tokenCache.EXPECT().
			IsRevoked(gomock.Any(), "jti-revoked").
			Return(true, nil)

		_, err := client.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
			AccessToken: "revoked.token",
		})

		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("Redis down — fail open, returns OK", func(t *testing.T) {
		client, tokenMgr, tokenCache := setupGRPC(t)

		tokenMgr.EXPECT().
			VerifyAccess("valid.token").
			Return("user-456", "jti-xyz", nil)

		tokenCache.EXPECT().
			IsRevoked(gomock.Any(), "jti-xyz").
			Return(false, assert.AnError)

		resp, err := client.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
			AccessToken: "valid.token",
		})

		require.NoError(t, err)
		assert.Equal(t, "user-456", resp.UserId)
	})

	t.Run("empty token returns UNAUTHENTICATED", func(t *testing.T) {
		client, tokenMgr, _ := setupGRPC(t)

		tokenMgr.EXPECT().
			VerifyAccess("").
			Return("", "", entity.ErrInvalidPassword)

		_, err := client.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
			AccessToken: "",
		})

		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("cancelled context is respected", func(t *testing.T) {
		client, _, _ := setupGRPC(t)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := client.ValidateToken(ctx, &pb.ValidateTokenRequest{
			AccessToken: "any",
		})

		require.Error(t, err)
		assert.Equal(t, codes.Canceled, status.Code(err))
	})
}

func TestJWTManagerContract(t *testing.T) {
	const secret = "super-secret-key-for-tests-32chr"
	mgr := usecase.NewJWTManager(secret)

	t.Run("generate then verify returns same user_id and jti", func(t *testing.T) {
		tok, jti, err := mgr.GenerateAccess("user-abc", 15*time.Minute)
		require.NoError(t, err)
		assert.NotEmpty(t, tok)
		assert.NotEmpty(t, jti)

		gotUser, gotJTI, err := mgr.VerifyAccess(tok)
		require.NoError(t, err)
		assert.Equal(t, "user-abc", gotUser)
		assert.Equal(t, jti, gotJTI)
	})

	t.Run("tampered signature returns error", func(t *testing.T) {
		_, _, err := mgr.VerifyAccess("header.payload.badsignature")
		assert.Error(t, err)
	})

	t.Run("expired token returns error", func(t *testing.T) {
		tok, _, err := mgr.GenerateAccess("user-xyz", -time.Second)
		require.NoError(t, err)

		_, _, err = mgr.VerifyAccess(tok)
		assert.Error(t, err)
	})

	t.Run("token signed with different secret returns error", func(t *testing.T) {
		other := usecase.NewJWTManager("completely-different-secret-32ch")
		tok, _, err := other.GenerateAccess("user-abc", 15*time.Minute)
		require.NoError(t, err)

		_, _, err = mgr.VerifyAccess(tok)
		assert.Error(t, err)
	})

	t.Run("different users get different jtis", func(t *testing.T) {
		_, jti1, err := mgr.GenerateAccess("user-1", time.Minute)
		require.NoError(t, err)

		_, jti2, err := mgr.GenerateAccess("user-2", time.Minute)
		require.NoError(t, err)

		assert.NotEqual(t, jti1, jti2)
	})
}
