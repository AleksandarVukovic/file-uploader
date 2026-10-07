package api

import (
	"log/slog"
	"net/http"

	"github.com/aleksandarv/file-uploader/common/grpc/interceptor"
	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/pb"
	userssvr "github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/server"
	"github.com/aleksandarv/file-uploader/user-service/gen/health"
	healthsvr "github.com/aleksandarv/file-uploader/user-service/gen/http/health/server"
	"github.com/aleksandarv/file-uploader/user-service/gen/users"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/grpc"
)

func GRPCServer(log *slog.Logger, usersSvc users.Service, opts ...grpc.ServerOption) *grpc.Server {
	opts = append(opts, grpc.ChainUnaryInterceptor(
		interceptor.Recover(log),
		interceptor.RequestID(true),
		interceptor.Logger(log, true),
	))
	srv := grpc.NewServer(opts...)
	userspb.RegisterUsersServer(srv, userssvr.New(users.NewEndpoints(usersSvc), nil))
	for name := range srv.GetServiceInfo() {
		log.Debug("expose gRPC service", "service", name)
	}
	return srv
}

func HealthRoutes(log *slog.Logger, healthSvc health.Service) http.Handler {
	mux := goahttp.NewMuxer()

	healthSrv := healthsvr.New(health.NewEndpoints(healthSvc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	healthSrv.Use(httpmiddleware.PanicHandler())
	healthSrv.Use(logger.RequestMiddleware(log, false))
	healthSrv.Mount(mux)
	for _, m := range healthSrv.Mounts {
		log.Debug("expose health API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	return mux
}
