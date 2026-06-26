package oracleclient

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/asolovov/evm-oracle-demo-api/config"
	commonv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/common/v1"
	oraclev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/oracle/v1"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

func TestDialRejectsEmptyAddr(t *testing.T) {
	_, err := Dial(config.GRPCClientConfig{})
	if err == nil || !contains(err.Error(), "oracle_service_addr") {
		t.Fatalf("expected oracle_service_addr validation error, got %v", err)
	}
}

func TestGetSubmissionMapsErrors(t *testing.T) {
	client, closer := startStubServer(t, &oracleStub{notFound: true})
	t.Cleanup(closer)
	if _, err := client.GetSubmissionByReqID(context.Background(), "42"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	client2, closer2 := startStubServer(t, &oracleStub{invalidArg: true})
	t.Cleanup(closer2)
	if _, err := client2.GetSubmissionByReqID(context.Background(), ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestGetSubmissionByReqIDAndTxHash(t *testing.T) {
	stub := &oracleStub{
		sub: &oraclev1.SubmissionStatus{
			ReqId:   "42",
			AssetId: "weth",
			Status:  oraclev1.SubmissionStatus_STATUS_EXPIRED,
		},
	}
	client, closer := startStubServer(t, stub)
	t.Cleanup(closer)

	got, err := client.GetSubmissionByReqID(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetSubmissionByReqID: %v", err)
	}
	if got.Status != models.SubmissionStatusExpired {
		t.Fatalf("status = %q, want expired", got.Status)
	}
	if stub.lastReq.GetReqId() != "42" || stub.lastReq.GetTxHash() != "" {
		t.Fatalf("expected lookup by req_id, got %+v", stub.lastReq)
	}

	if _, err := client.GetSubmissionByTxHash(context.Background(), "0xabc"); err != nil {
		t.Fatalf("GetSubmissionByTxHash: %v", err)
	}
	if stub.lastReq.GetTxHash() != "0xabc" || stub.lastReq.GetReqId() != "" {
		t.Fatalf("expected lookup by tx_hash, got %+v", stub.lastReq)
	}
}

func TestListSubmissionsConvertsPagination(t *testing.T) {
	stub := &oracleStub{
		list: &oraclev1.ListSubmissionsResponse{
			Submissions: []*oraclev1.SubmissionStatus{
				{ReqId: "1", AssetId: "weth", Status: oraclev1.SubmissionStatus_STATUS_CONFIRMED},
				{ReqId: "0", AssetId: "wbtc", Status: oraclev1.SubmissionStatus_STATUS_DROPPED},
			},
			Page: &commonv1.PageResponse{Page: 1, PageSize: 50, TotalCount: 2, TotalPages: 1},
		},
	}
	client, closer := startStubServer(t, stub)
	t.Cleanup(closer)

	subs, page, err := client.ListSubmissions(context.Background(), ListSubmissionsFilter{
		AssetID: "weth",
		Page:    Page{Number: 1, Size: 50},
	})
	if err != nil {
		t.Fatalf("ListSubmissions: %v", err)
	}
	if len(subs) != 2 || subs[0].Status != models.SubmissionStatusConfirmed {
		t.Fatalf("unexpected submissions: %+v", subs)
	}
	if page.TotalItems != 2 || page.Number != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	if stub.lastList.GetAssetId() != "weth" || stub.lastList.GetPage().GetPageSize() != 50 {
		t.Fatalf("filter not translated: %+v", stub.lastList)
	}
}

// --- stub server --------------------------------------------------------

type oracleStub struct {
	oraclev1.UnimplementedOracleServiceServer
	sub        *oraclev1.SubmissionStatus
	list       *oraclev1.ListSubmissionsResponse
	notFound   bool
	invalidArg bool
	lastReq    *oraclev1.GetSubmissionStatusRequest
	lastList   *oraclev1.ListSubmissionsRequest
}

func (s *oracleStub) GetSubmissionStatus(_ context.Context, req *oraclev1.GetSubmissionStatusRequest) (*oraclev1.SubmissionStatus, error) {
	s.lastReq = req
	if s.notFound {
		return nil, status.Error(codes.NotFound, "no such submission")
	}
	if s.invalidArg {
		return nil, status.Error(codes.InvalidArgument, "exactly one of req_id/tx_hash")
	}
	return s.sub, nil
}

func (s *oracleStub) ListSubmissions(_ context.Context, req *oraclev1.ListSubmissionsRequest) (*oraclev1.ListSubmissionsResponse, error) {
	s.lastList = req
	return s.list, nil
}

func startStubServer(t *testing.T, impl oraclev1.OracleServiceServer) (Client, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	oraclev1.RegisterOracleServiceServer(srv, impl)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("bufconn dial: %v", err)
	}
	c := &grpcClient{conn: conn, client: oraclev1.NewOracleServiceClient(conn)}
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
