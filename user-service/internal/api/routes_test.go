package api

import (
	"context"
	"log/slog"
	"net"
	"testing"

	"github.com/aleksandarv/file-uploader/common/grpc/interceptor"
	"github.com/aleksandarv/file-uploader/common/logger"
	userspb "github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/pb"
	healthsvr "github.com/aleksandarv/file-uploader/user-service/gen/http/health/server"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

var healthPath = healthsvr.HealthHealthPath()

const testRequestID = "test-req-id"

func newGRPCClient(t *testing.T, repo *mockRepo) userspb.UsersClient {
	t.Helper()

	withRequestID := grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, interceptor.RequestIDMetadataKey, testRequestID)
		return invoker(ctx, method, req, reply, cc, opts...)
	})
	return newGRPCClientWithLogger(t, logger.NewLogger(false), repo, withRequestID)
}

func newGRPCClientWithLogger(t *testing.T, log *slog.Logger, repo *mockRepo, dialOpts ...grpc.DialOption) userspb.UsersClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := GRPCServer(log, NewUsersHandler(userssvc.New(repo)))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	dialOpts = append(dialOpts,
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	conn, err := grpc.NewClient("passthrough:///bufnet", dialOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return userspb.NewUsersClient(conn)
}

func strPtr(s string) *string { return &s }
