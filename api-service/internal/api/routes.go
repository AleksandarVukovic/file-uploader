package api

import (
	"log/slog"
	"net/http"

	"github.com/aleksandarv/file-uploader/api-service/gen/files"
	"github.com/aleksandarv/file-uploader/api-service/gen/health"
	filessvr "github.com/aleksandarv/file-uploader/api-service/gen/http/files/server"
	healthsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/health/server"
	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/aleksandarv/file-uploader/common/logger"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/http/middleware"
)

func Routes(log *slog.Logger, filesSvc files.Service, healthSvc health.Service) http.Handler {
	mux := goahttp.NewMuxer()

	filesSrv := filessvr.New(files.NewEndpoints(filesSvc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	filesSrv.Use(httpmiddleware.PanicHandler())
	filesSrv.Use(logger.RequestMiddleware(log))
	filesSrv.Use(middleware.PopulateRequestContext())
	filesSrv.Use(middleware.RequestID(
		middleware.UseXRequestIDHeaderOption(true),
		middleware.XRequestHeaderLimitOption(64),
	))
	filesSrv.Mount(mux)
	for _, m := range filesSrv.Mounts {
		log.Debug("expose API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	healthSrv := healthsvr.New(health.NewEndpoints(healthSvc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	healthSrv.Use(httpmiddleware.PanicHandler())
	healthSrv.Use(logger.RequestMiddleware(log))
	healthSrv.Use(middleware.PopulateRequestContext())
	healthSrv.Use(middleware.RequestID(
		middleware.UseXRequestIDHeaderOption(true),
		middleware.XRequestHeaderLimitOption(64),
	))
	healthSrv.Mount(mux)
	for _, m := range healthSrv.Mounts {
		log.Debug("expose health API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	return mux
}
