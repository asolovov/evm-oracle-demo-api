package indexerclient

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
	indexerv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/indexer/v1"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

func TestDialRejectsEmptyAddr(t *testing.T) {
	_, err := Dial(config.GRPCClientConfig{})
	if err == nil || !contains(err.Error(), "indexer_service_addr") {
		t.Fatalf("expected indexer_service_addr validation error, got %v", err)
	}
}

func TestGetRequestMapsNotFound(t *testing.T) {
	client, closer := startStubServer(t, &indexerStub{notFound: true})
	t.Cleanup(closer)

	_, err := client.GetRequest(context.Background(), "42")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListEventsConvertsKindsAndPagination(t *testing.T) {
	stub := &indexerStub{
		listResp: &indexerv1.ListEventsResponse{
			Events: []*indexerv1.Event{
				{
					Kind: indexerv1.EventKind_EVENT_KIND_PRICE_REQUESTED,
					Payload: &indexerv1.Event_PriceRequested{
						PriceRequested: &indexerv1.PriceRequestedEvent{ReqId: "42", AssetId: "weth"},
					},
				},
			},
			Page: &commonv1.PageResponse{Page: 1, PageSize: 50, TotalCount: 100, TotalPages: 2},
		},
	}
	client, closer := startStubServer(t, stub)
	t.Cleanup(closer)

	got, page, err := client.ListEvents(context.Background(), ListEventsFilter{
		Kinds:   []models.EventKind{models.EventKindPriceRequested},
		AssetID: "weth",
		Page:    Page{Number: 1, Size: 50},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || got[0].Kind != models.EventKindPriceRequested {
		t.Fatalf("ListEvents events: %+v", got)
	}
	if page.Number != 1 || page.Size != 50 || page.TotalItems != 100 || page.TotalPages != 2 {
		t.Fatalf("ListEvents page: %+v", page)
	}
	// Sanity check that the stub received the proto-translated request.
	if len(stub.lastListReq.GetKinds()) != 1 ||
		stub.lastListReq.GetKinds()[0] != indexerv1.EventKind_EVENT_KIND_PRICE_REQUESTED {
		t.Fatalf("expected kinds to be translated to proto, got %+v", stub.lastListReq.GetKinds())
	}
	if stub.lastListReq.GetPage().GetPage() != 1 || stub.lastListReq.GetPage().GetPageSize() != 50 {
		t.Fatalf("expected page translation, got %+v", stub.lastListReq.GetPage())
	}
}

// --- stub server --------------------------------------------------------

type indexerStub struct {
	indexerv1.UnimplementedIndexerServiceServer
	listResp    *indexerv1.ListEventsResponse
	notFound    bool
	lastListReq *indexerv1.ListEventsRequest
}

func (s *indexerStub) ListEvents(_ context.Context, req *indexerv1.ListEventsRequest) (*indexerv1.ListEventsResponse, error) {
	s.lastListReq = req
	return s.listResp, nil
}

func (s *indexerStub) GetRequest(_ context.Context, _ *indexerv1.GetRequestRequest) (*indexerv1.RequestStatus, error) {
	if s.notFound {
		return nil, status.Error(codes.NotFound, "not observed")
	}
	return &indexerv1.RequestStatus{ReqId: "42"}, nil
}

func startStubServer(t *testing.T, impl indexerv1.IndexerServiceServer) (Client, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	indexerv1.RegisterIndexerServiceServer(srv, impl)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("bufconn dial: %v", err)
	}
	c := &grpcClient{conn: conn, client: indexerv1.NewIndexerServiceClient(conn)}
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
