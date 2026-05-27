// Package indexerclient wraps the indexer-service gRPC client. External
// service clients are plain packages, not template modules (architecture
// rule 5).
package indexerclient

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asolovov/evm-oracle-demo-api/config"
	commonv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/common/v1"
	indexerv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/indexer/v1"
	"github.com/asolovov/evm-oracle-demo-api/internal/grpcdial"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// ErrNotFound surfaces a gRPC NotFound to handlers as a domain sentinel.
var ErrNotFound = errors.New("request not observed by indexer")

// ListEventsFilter mirrors indexer.v1.ListEventsRequest's filter surface.
type ListEventsFilter struct {
	Kinds     []models.EventKind
	AssetID   string
	FromBlock uint64
	ToBlock   uint64
	Page      Page
}

// Page mirrors common.v1.PageRequest in the domain.
type Page struct {
	Number int32
	Size   int32
}

// PageInfo mirrors common.v1.PageResponse in the domain.
type PageInfo struct {
	Number     int32
	Size       int32
	TotalItems int64
	TotalPages int32
}

// StreamEventsFilter mirrors indexer.v1.StreamEventsRequest.
type StreamEventsFilter struct {
	Kinds     []models.EventKind
	AssetID   string
	FromBlock uint64
}

// Client is the read + stream surface handlers and the WS hub consume.
type Client interface {
	ListEvents(ctx context.Context, filter ListEventsFilter) ([]models.Event, PageInfo, error)
	GetRequest(ctx context.Context, reqID string) (models.RequestSummary, error)
	StreamEvents(ctx context.Context, filter StreamEventsFilter, onMsg func(models.Event)) error
	Close() error
}

type grpcClient struct {
	conn   *grpc.ClientConn
	client indexerv1.IndexerServiceClient
}

// Dial constructs the gRPC client. grpc.NewClient is non-blocking — the
// connection is opened lazily on first RPC, so a temporarily-unreachable
// upstream is recovered automatically once it comes back up.
func Dial(cfg config.GRPCClientConfig) (Client, error) {
	if cfg.IndexerServiceAddr == "" {
		return nil, errors.New("indexer_service_addr is required")
	}
	opts, err := grpcdial.Options(cfg)
	if err != nil {
		return nil, fmt.Errorf("indexer-client dial options: %w", err)
	}

	conn, err := grpc.NewClient(cfg.IndexerServiceAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial indexer-service at %s: %w", cfg.IndexerServiceAddr, err)
	}

	return &grpcClient{conn: conn, client: indexerv1.NewIndexerServiceClient(conn)}, nil
}

// ListEvents paginates the historical event log.
func (c *grpcClient) ListEvents(ctx context.Context, filter ListEventsFilter) ([]models.Event, PageInfo, error) {
	req := &indexerv1.ListEventsRequest{
		Kinds:     toProtoKinds(filter.Kinds),
		AssetId:   filter.AssetID,
		FromBlock: filter.FromBlock,
		ToBlock:   filter.ToBlock,
		Page: &commonv1.PageRequest{
			Page:     filter.Page.Number,
			PageSize: filter.Page.Size,
		},
	}
	resp, err := c.client.ListEvents(ctx, req)
	if err != nil {
		return nil, PageInfo{}, fmt.Errorf("indexer.ListEvents: %w", err)
	}
	out := make([]models.Event, 0, len(resp.GetEvents()))
	for _, e := range resp.GetEvents() {
		out = append(out, models.EventFromProto(e))
	}
	return out, pageInfoFromProto(resp.GetPage()), nil
}

// GetRequest returns the joined request lifecycle by req_id.
func (c *grpcClient) GetRequest(ctx context.Context, reqID string) (models.RequestSummary, error) {
	resp, err := c.client.GetRequest(ctx, &indexerv1.GetRequestRequest{ReqId: reqID})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			return models.RequestSummary{}, ErrNotFound
		}
		return models.RequestSummary{}, fmt.Errorf("indexer.GetRequest(%s): %w", reqID, err)
	}
	return models.RequestSummaryFromProto(resp), nil
}

// StreamEvents drives one long-lived indexer.StreamEvents stream and forwards
// every event past the indexer's confirmation threshold to onMsg. Returns
// when ctx is done, the server closes the stream, or the stream errors out.
// Callers are responsible for retry/reconnect.
func (c *grpcClient) StreamEvents(ctx context.Context, filter StreamEventsFilter, onMsg func(models.Event)) error {
	req := &indexerv1.StreamEventsRequest{
		Kinds:     toProtoKinds(filter.Kinds),
		AssetId:   filter.AssetID,
		FromBlock: filter.FromBlock,
	}
	stream, err := c.client.StreamEvents(ctx, req)
	if err != nil {
		return fmt.Errorf("indexer.StreamEvents: %w", err)
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
			return fmt.Errorf("indexer.StreamEvents.Recv: %w", err)
		}
		if onMsg != nil {
			onMsg(models.EventFromProto(msg))
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

func toProtoKinds(kinds []models.EventKind) []indexerv1.EventKind {
	if len(kinds) == 0 {
		return nil
	}
	out := make([]indexerv1.EventKind, 0, len(kinds))
	for _, k := range kinds {
		if pk := models.EventKindToProto(k); pk != indexerv1.EventKind_EVENT_KIND_UNSPECIFIED {
			out = append(out, pk)
		}
	}
	return out
}

func pageInfoFromProto(p *commonv1.PageResponse) PageInfo {
	if p == nil {
		return PageInfo{}
	}
	return PageInfo{
		Number:     p.GetPage(),
		Size:       p.GetPageSize(),
		TotalItems: int64(p.GetTotalCount()),
		TotalPages: p.GetTotalPages(),
	}
}
