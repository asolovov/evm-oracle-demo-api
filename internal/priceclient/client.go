// Package priceclient wraps the price-service gRPC client. External service
// clients are plain packages, not template modules (architecture rule 5).
package priceclient

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asolovov/evm-oracle-demo-api/config"
	pricev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/price/v1"
	"github.com/asolovov/evm-oracle-demo-api/internal/grpcdial"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// ErrNotFound surfaces a gRPC NotFound to handlers as a domain sentinel.
var ErrNotFound = errors.New("asset not tracked by price-service")

// Client is the read surface handlers and the WS hub consume.
type Client interface {
	GetPrice(ctx context.Context, assetID string) (models.AggregatedPrice, error)
	Subscribe(ctx context.Context, assetIDs []string, onMsg func(models.AggregatedPrice)) error
	Close() error
}

// grpcClient wires the generated stub into the Client interface.
type grpcClient struct {
	conn   *grpc.ClientConn
	client pricev1.PriceServiceClient
}

// Dial constructs the gRPC client. grpc.NewClient is non-blocking — the
// connection is opened lazily on first RPC, so a temporarily-unreachable
// upstream is recovered automatically once it comes back up.
func Dial(cfg config.GRPCClientConfig) (Client, error) {
	if cfg.PriceServiceAddr == "" {
		return nil, errors.New("price_service_addr is required")
	}
	opts, err := grpcdial.Options(cfg)
	if err != nil {
		return nil, fmt.Errorf("price-client dial options: %w", err)
	}

	conn, err := grpc.NewClient(cfg.PriceServiceAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial price-service at %s: %w", cfg.PriceServiceAddr, err)
	}

	return &grpcClient{conn: conn, client: pricev1.NewPriceServiceClient(conn)}, nil
}

// GetPrice returns the latest aggregated price for assetID.
func (c *grpcClient) GetPrice(ctx context.Context, assetID string) (models.AggregatedPrice, error) {
	resp, err := c.client.GetPrice(ctx, &pricev1.GetPriceRequest{AssetId: assetID})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			return models.AggregatedPrice{}, ErrNotFound
		}
		return models.AggregatedPrice{}, fmt.Errorf("price.GetPrice(%s): %w", assetID, err)
	}
	return models.AggregatedPriceFromProto(resp), nil
}

// Subscribe drives one long-lived price.Subscribe stream and forwards every
// delivered AggregatedPrice to onMsg. Returns when ctx is done, the server
// closes the stream, or the stream errors out. Callers are responsible for
// retry/reconnect.
func (c *grpcClient) Subscribe(ctx context.Context, assetIDs []string, onMsg func(models.AggregatedPrice)) error {
	if len(assetIDs) == 0 {
		return errors.New("subscribe requires at least one asset id")
	}
	stream, err := c.client.Subscribe(ctx, &pricev1.SubscribeRequest{AssetIds: assetIDs})
	if err != nil {
		return fmt.Errorf("price.Subscribe: %w", err)
	}
	for {
		msg, err := stream.Recv()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("price.Subscribe.Recv: %w", err)
		}
		if onMsg != nil {
			onMsg(models.AggregatedPriceFromProto(msg))
		}
	}
}

// Close terminates the underlying gRPC connection.
func (c *grpcClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
