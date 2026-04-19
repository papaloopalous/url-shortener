package auth

import (
	"context"
	"fmt"

	authpb "gateway-service/proto/auth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpc.ClientConn
	client authpb.AuthServiceClient
}

type AuthClient interface {
	ValidateToken(ctx context.Context, token string) (userID string, err error)
}

func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial auth-service %q: %w", addr, err)
	}
	return &Client{
		conn:   conn,
		client: authpb.NewAuthServiceClient(conn),
	}, nil
}

func (c *Client) ValidateToken(ctx context.Context, token string) (string, error) {
	resp, err := c.client.ValidateToken(ctx, &authpb.ValidateTokenRequest{AccessToken: token})
	if err != nil {
		return "", fmt.Errorf("validate token: %w", err)
	}
	return resp.GetUserId(), nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}
