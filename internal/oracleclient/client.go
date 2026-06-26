// Package oracleclient wraps the oracle-service gRPC client. External service
// clients are plain packages, not template modules (architecture rule 5).
//
// The BFF consumes oracle.v1 read-only: GetSubmissionStatus + ListSubmissions
// back the dashboard's submission-history surface. SetHeartbeat is admin-only
// and deliberately NOT exposed through this public BFF.
package oracleclient

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asolovov/evm-oracle-demo-api/config"
	commonv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/common/v1"
	oraclev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/oracle/v1"
	"github.com/asolovov/evm-oracle-demo-api/internal/grpcdial"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// ErrNotFound surfaces a gRPC NotFound to handlers as a domain sentinel.
var ErrNotFound = errors.New("submission not found")

// ErrInvalidArgument surfaces a gRPC InvalidArgument (e.g. both/neither of
// req_id + tx_hash supplied) as a domain sentinel so the handler can 400.
var ErrInvalidArgument = errors.New("invalid submission lookup key")

// ListSubmissionsFilter mirrors oracle.v1.ListSubmissionsRequest.
type ListSubmissionsFilter struct {
	AssetID string
	Page    Page
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

// Client is the read surface handlers consume.
type Client interface {
	GetSubmissionByReqID(ctx context.Context, reqID string) (models.SubmissionStatus, error)
	GetSubmissionByTxHash(ctx context.Context, txHash string) (models.SubmissionStatus, error)
	ListSubmissions(ctx context.Context, filter ListSubmissionsFilter) ([]models.SubmissionStatus, PageInfo, error)
	Close() error
}

type grpcClient struct {
	conn   *grpc.ClientConn
	client oraclev1.OracleServiceClient
}

// Dial constructs the gRPC client. grpc.NewClient is non-blocking — the
// connection is opened lazily on first RPC, so a temporarily-unreachable
// upstream is recovered automatically once it comes back up.
func Dial(cfg config.GRPCClientConfig) (Client, error) {
	if cfg.OracleServiceAddr == "" {
		return nil, errors.New("oracle_service_addr is required")
	}
	opts, err := grpcdial.Options(cfg)
	if err != nil {
		return nil, fmt.Errorf("oracle-client dial options: %w", err)
	}

	conn, err := grpc.NewClient(cfg.OracleServiceAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial oracle-service at %s: %w", cfg.OracleServiceAddr, err)
	}

	return &grpcClient{conn: conn, client: oraclev1.NewOracleServiceClient(conn)}, nil
}

// GetSubmissionByReqID looks a submission up by its consumer-issued req_id.
func (c *grpcClient) GetSubmissionByReqID(ctx context.Context, reqID string) (models.SubmissionStatus, error) {
	return c.getSubmission(ctx, &oraclev1.GetSubmissionStatusRequest{ReqId: reqID})
}

// GetSubmissionByTxHash looks a submission up by tx hash — the only way to
// reach heartbeat submissions (which carry req_id = "0").
func (c *grpcClient) GetSubmissionByTxHash(ctx context.Context, txHash string) (models.SubmissionStatus, error) {
	return c.getSubmission(ctx, &oraclev1.GetSubmissionStatusRequest{TxHash: txHash})
}

func (c *grpcClient) getSubmission(ctx context.Context, req *oraclev1.GetSubmissionStatusRequest) (models.SubmissionStatus, error) {
	resp, err := c.client.GetSubmissionStatus(ctx, req)
	if err != nil {
		if s, ok := status.FromError(err); ok {
			//nolint:exhaustive // only NotFound + InvalidArgument map to domain
			// sentinels; every other code falls through to the wrapped error below.
			switch s.Code() {
			case codes.NotFound:
				return models.SubmissionStatus{}, ErrNotFound
			case codes.InvalidArgument:
				return models.SubmissionStatus{}, ErrInvalidArgument
			}
		}
		return models.SubmissionStatus{}, fmt.Errorf("oracle.GetSubmissionStatus: %w", err)
	}
	return models.SubmissionStatusFromProto(resp), nil
}

// ListSubmissions pages over submission history in descending submitted_at
// order, optionally filtered by asset.
func (c *grpcClient) ListSubmissions(ctx context.Context, filter ListSubmissionsFilter) ([]models.SubmissionStatus, PageInfo, error) {
	resp, err := c.client.ListSubmissions(ctx, &oraclev1.ListSubmissionsRequest{
		AssetId: filter.AssetID,
		Page: &commonv1.PageRequest{
			Page:     filter.Page.Number,
			PageSize: filter.Page.Size,
		},
	})
	if err != nil {
		return nil, PageInfo{}, fmt.Errorf("oracle.ListSubmissions: %w", err)
	}
	out := make([]models.SubmissionStatus, 0, len(resp.GetSubmissions()))
	for _, s := range resp.GetSubmissions() {
		out = append(out, models.SubmissionStatusFromProto(s))
	}
	return out, pageInfoFromProto(resp.GetPage()), nil
}

// Close terminates the underlying gRPC connection.
func (c *grpcClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
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
