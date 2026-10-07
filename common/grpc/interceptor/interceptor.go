package interceptor

import (
	"context"
	"log/slog"
	"runtime/debug"

	"github.com/aleksandarv/file-uploader/common/logger"
	goam "goa.design/goa/v3/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const RequestIDMetadataKey = "x-request-id"

func ClientRequestID() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if reqID, ok := ctx.Value(goam.RequestIDKey).(string); ok && reqID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, RequestIDMetadataKey, reqID)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func RequestID(required bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var reqID string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if values := md.Get(RequestIDMetadataKey); len(values) > 0 {
				reqID = values[0]
			}
		}

		if reqID == "" {
			if required {
				return nil, status.Error(codes.InvalidArgument, "missing "+RequestIDMetadataKey+" metadata")
			}
			return handler(ctx, req)
		}
		return handler(context.WithValue(ctx, goam.RequestIDKey, reqID), req)
	}
}

func Logger(log *slog.Logger, reqIDMandatory bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		rlog := log
		if reqID, ok := ctx.Value(goam.RequestIDKey).(string); ok {
			rlog = logger.WithRequestID(log, reqID)
		} else if reqIDMandatory {
			panic("interceptor: context without request ID")
		}
		return handler(logger.WithCtx(ctx, rlog), req)
	}
}

func Recover(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic in gRPC handler", "method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}
