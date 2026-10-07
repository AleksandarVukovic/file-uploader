//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aleksandarv/file-uploader/common/grpc/interceptor"
	"github.com/aleksandarv/file-uploader/common/logger"
	userspb "github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/pb"
	"github.com/aleksandarv/file-uploader/user-service/gen/health"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type panicHealthService struct{}

func (panicHealthService) Health(context.Context) (*health.HealthResult, error) {
	panic("boom: health handler panicked")
}

func TestGRPCServer_PanicInHandlerYieldsInternal(t *testing.T) {
	t.Parallel()

	repo := &mockRepo{}
	repo.On("GetByUsername", mock.Anything, testUsername).Run(func(mock.Arguments) {
		panic("boom")
	}).Return(userssvc.User{}, nil)
	client := newGRPCClient(t, repo)

	_, err := client.GetByUsername(context.Background(), &userspb.GetByUsernameRequest{Username: strPtr(testUsername)})

	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPCServer_HandlerContextCarriesLogger(t *testing.T) {
	t.Parallel()

	repo := &mockRepo{}
	repo.On("GetByUsername", mock.Anything, testUsername).Run(func(args mock.Arguments) {
		logger.FromCtx(args.Get(0).(context.Context))
	}).Return(userssvc.User{}, userssvc.ErrNotFound)
	client := newGRPCClient(t, repo)

	_, err := client.GetByUsername(context.Background(), &userspb.GetByUsernameRequest{Username: strPtr(testUsername)})

	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPCServer_RequestIDFromMetadataIsLogged(t *testing.T) {
	t.Parallel()

	var out syncBuffer
	repo := &mockRepo{}
	repo.On("GetByUsername", mock.Anything, testUsername).Run(func(args mock.Arguments) {
		logger.FromCtx(args.Get(0).(context.Context)).Info("probe")
	}).Return(userssvc.User{}, userssvc.ErrNotFound)
	client := newGRPCClientWithLogger(t, slog.New(slog.NewJSONHandler(&out, nil)), repo)

	ctx := metadata.AppendToOutgoingContext(context.Background(), interceptor.RequestIDMetadataKey, "req-abc")
	_, err := client.GetByUsername(ctx, &userspb.GetByUsernameRequest{Username: strPtr(testUsername)})

	require.Equal(t, codes.NotFound, status.Code(err))
	require.Contains(t, out.String(), `"msg":"probe"`)
	require.Contains(t, out.String(), `"reqID":"req-abc"`)
}

func TestGRPCServer_RejectsCallsWithoutRequestID(t *testing.T) {
	t.Parallel()

	repo := &mockRepo{}
	client := newGRPCClientWithLogger(t, logger.NewLogger(false), repo)

	_, err := client.GetByUsername(context.Background(), &userspb.GetByUsernameRequest{Username: strPtr(testUsername)})

	require.Equal(t, codes.InvalidArgument, status.Code(err))
	repo.AssertNotCalled(t, "GetByUsername", mock.Anything, mock.Anything)
}

func TestHealthRoutes_ReturnsOK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(HealthRoutes(logger.NewLogger(false), NewHealthHandler()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "ok", body.Status)
}

func TestHealthRoutes_PanicIsRecoveredAs500(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(HealthRoutes(logger.NewLogger(false), panicHealthService{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
