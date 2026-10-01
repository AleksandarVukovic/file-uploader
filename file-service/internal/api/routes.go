package api

import (
	"log/slog"
	"net/http"

	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aleksandarv/file-uploader/file-service/gen/health"
	filessvr "github.com/aleksandarv/file-uploader/file-service/gen/http/files/server"
	healthsvr "github.com/aleksandarv/file-uploader/file-service/gen/http/health/server"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/http/middleware"
)

func Routes(log *slog.Logger, filesSvc files.Service) http.Handler {
	mux := goahttp.NewMuxer()

	filesSrv := filessvr.New(files.NewEndpoints(filesSvc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	filesSrv.Use(httpmiddleware.PanicHandler())
	filesSrv.Use(logger.RequestMiddleware(log, true))
	filesSrv.Use(httpmiddleware.RequireRequestID())
	filesSrv.Use(middleware.PopulateRequestContext())
	filesSrv.Mount(mux)
	for _, m := range filesSrv.Mounts {
		log.Debug("expose API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	return mux
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
