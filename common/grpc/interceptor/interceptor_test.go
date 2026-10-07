package interceptor

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/stretchr/testify/require"
	goam "goa.design/goa/v3/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func newBufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func incomingCtx(reqID string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(RequestIDMetadataKey, reqID))
}

func TestRequestID_PutsMetadataValueInContext(t *testing.T) {
	var got any

	_, err := RequestID(true)(incomingCtx("req-123"), "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		got = ctx.Value(goam.RequestIDKey)
		return "res", nil
	})

	require.NoError(t, err)
	require.Equal(t, "req-123", got)
}

func TestRequestID_Required(t *testing.T) {
	tests := map[string]context.Context{
		"no metadata":    context.Background(),
		"empty metadata": incomingCtx(""),
	}
	for name, ctx := range tests {
		t.Run(name, func(t *testing.T) {
			called := false

			_, err := RequestID(true)(ctx, "req", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
				called = true
				return nil, nil
			})

			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.False(t, called)
		})
	}
}

func TestRequestID_Optional(t *testing.T) {
	var got any

	_, err := RequestID(false)(context.Background(), "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		got = ctx.Value(goam.RequestIDKey)
		return "res", nil
	})

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestLogger_TagsLoggerWithRequestIDFromContext(t *testing.T) {
	log, buf := newBufferLogger()
	ctx := context.WithValue(context.Background(), goam.RequestIDKey, "req-123")

	_, err := Logger(log, true)(ctx, "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		logger.FromCtx(ctx).Info("handling")
		return "res", nil
	})

	require.NoError(t, err)
	require.Contains(t, buf.String(), `"reqID":"req-123"`)
}

func TestLogger_OptionalRequestID(t *testing.T) {
	log, buf := newBufferLogger()

	_, err := Logger(log, false)(context.Background(), "req", &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		logger.FromCtx(ctx).Info("handling")
		return "res", nil
	})

	require.NoError(t, err)
	require.Contains(t, buf.String(), "handling")
	require.NotContains(t, buf.String(), "reqID")
}

func TestLogger_MandatoryRequestIDMissingPanics(t *testing.T) {
	log, _ := newBufferLogger()

	require.Panics(t, func() {
		_, _ = Logger(log, true)(context.Background(), "req", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
			return nil, nil
		})
	})
}

func TestClientRequestID(t *testing.T) {
	invoke := func(ctx context.Context) metadata.MD {
		var md metadata.MD
		err := ClientRequestID()(ctx, "/users.Users/Authenticate", nil, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, _ = metadata.FromOutgoingContext(ctx)
			return nil
		})
		require.NoError(t, err)
		return md
	}

	t.Run("propagates the request ID from the context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), goam.RequestIDKey, "req-123")

		require.Equal(t, []string{"req-123"}, invoke(ctx).Get(RequestIDMetadataKey))
	})

	t.Run("sends nothing when the context has no request ID", func(t *testing.T) {
		require.Empty(t, invoke(context.Background()).Get(RequestIDMetadataKey))
	})
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
