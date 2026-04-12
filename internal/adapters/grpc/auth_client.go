package grpc

import (
	"context"
	"fmt"

	pb "shortener-service/proto/auth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type AuthGRPCClient struct {
	client pb.AuthServiceClient
}

func NewAuthGRPCClient(addr string) (*AuthGRPCClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial auth-service: %w", err)
	}
	return &AuthGRPCClient{
		client: pb.NewAuthServiceClient(conn),
	}, nil
}

func (c *AuthGRPCClient) ValidateToken(ctx context.Context, accessToken string) (string, error) {
	resp, err := c.client.ValidateToken(ctx, &pb.ValidateTokenRequest{
		AccessToken: accessToken,
	})
	if err != nil {
		return "", fmt.Errorf("validate token: %w", err)
	}
	return resp.UserId, nil
}
