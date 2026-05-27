package priceclient

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asolovov/evm-oracle-demo-api/config"
	pricev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/price/v1"
)

func TestDialRejectsEmptyAddr(t *testing.T) {
	_, err := Dial(config.GRPCClientConfig{})
	if err == nil || !contains(err.Error(), "price_service_addr") {
		t.Fatalf("expected price_service_addr validation error, got %v", err)
	}
}

func TestDialRejectsUnparseableKeepalive(t *testing.T) {
	_, err := Dial(config.GRPCClientConfig{
		PriceServiceAddr: "localhost:50051",
		KeepAlive: config.KeepAliveConfig{
			Time:    "not-a-duration",
			Timeout: "10s",
		},
	})
	if err == nil || !contains(err.Error(), "keep_alive.time") {
		t.Fatalf("expected keepalive parse error, got %v", err)
	}
}

func TestGetPriceMapsNotFound(t *testing.T) {
	client, closer := startStubServer(t, &priceStub{notFound: true})
	t.Cleanup(closer)

	_, err := client.GetPrice(context.Background(), "weth")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetPriceConvertsProtoToDomain(t *testing.T) {
	now := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	stub := &priceStub{
		price: &pricev1.AggregatedPrice{
			AssetId:      "weth",
			MedianPrice:  3450.20,
			AggregatedAt: timestamppb.New(now),
		},
	}
	client, closer := startStubServer(t, stub)
	t.Cleanup(closer)

	got, err := client.GetPrice(context.Background(), "weth")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if got.AssetID != "weth" || got.MedianPrice != 3450.20 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// --- stub server --------------------------------------------------------

type priceStub struct {
	pricev1.UnimplementedPriceServiceServer
	price    *pricev1.AggregatedPrice
	notFound bool
}

func (s *priceStub) GetPrice(_ context.Context, _ *pricev1.GetPriceRequest) (*pricev1.AggregatedPrice, error) {
	if s.notFound {
		return nil, status.Error(codes.NotFound, "not tracked")
	}
	return s.price, nil
}

func startStubServer(t *testing.T, impl pricev1.PriceServiceServer) (Client, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pricev1.RegisterPriceServiceServer(srv, impl)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("bufconn dial: %v", err)
	}
	c := &grpcClient{conn: conn, client: pricev1.NewPriceServiceClient(conn)}
	return c, func() { _ = conn.Close(); srv.Stop() }
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
