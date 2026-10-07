package interceptor

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newBufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func TestLogger_AttachesLoggerToHandlerContext(t *testing.T) {
	log, _ := newBufferLogger()

	var got *slog.Logger
	_, err := Logger(log)(context.Background(), "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		got = logger.FromCtx(ctx)
		return "res", nil
	})

	require.NoError(t, err)
	require.Same(t, log, got)
}

func TestRecover_ConvertsPanicToInternalError(t *testing.T) {
	log, buf := newBufferLogger()

	res, err := Recover(log)(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/users.Users/Create"}, func(context.Context, any) (any, error) {
		panic("boom")
	})

	require.Nil(t, res)
	require.Equal(t, codes.Internal, status.Code(err))
	require.NotContains(t, err.Error(), "boom")
	require.Contains(t, buf.String(), "boom")
	require.Contains(t, buf.String(), "/users.Users/Create")
}

func TestRecover_PassesThroughNormalResults(t *testing.T) {
	log, _ := newBufferLogger()

	res, err := Recover(log)(context.Background(), "req", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		return "res", nil
	})

	require.NoError(t, err)
	require.Equal(t, "res", res)
}
