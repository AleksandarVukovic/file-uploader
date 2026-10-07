package api

import (
	"log/slog"
	"net/http"

	"github.com/aleksandarv/file-uploader/api-service/gen/auth"
	"github.com/aleksandarv/file-uploader/api-service/gen/files"
	"github.com/aleksandarv/file-uploader/api-service/gen/health"
	authsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/auth/server"
	filessvr "github.com/aleksandarv/file-uploader/api-service/gen/http/files/server"
	healthsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/health/server"
	apimiddleware "github.com/aleksandarv/file-uploader/api-service/internal/middleware"
	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/http/middleware"
)

func Routes(log *slog.Logger, jwtSecret []byte, filesSvc files.Service, healthSvc health.Service, authSvc auth.Service) http.Handler {
	mux := goahttp.NewMuxer()

	authSrv := authsvr.New(auth.NewEndpoints(authSvc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	authSrv.Use(httpmiddleware.PanicHandler())
	authSrv.Use(httpmiddleware.Logger(log, true))
	authSrv.Use(middleware.PopulateRequestContext())
	authSrv.Use(middleware.RequestID(
		middleware.UseXRequestIDHeaderOption(true),
		middleware.XRequestHeaderLimitOption(64),
	))
	authSrv.Mount(mux)
	for _, m := range authSrv.Mounts {
		log.Debug("expose auth API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	filesSrv := filessvr.New(files.NewEndpoints(filesSvc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	filesSrv.Use(apimiddleware.JWT(jwtSecret))
	filesSrv.Use(httpmiddleware.PanicHandler())
	filesSrv.Use(httpmiddleware.Logger(log, true))
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
	healthSrv.Use(httpmiddleware.Logger(log, false))
	// healthSrv.Use(middleware.PopulateRequestContext())
	healthSrv.Mount(mux)
	for _, m := range healthSrv.Mounts {
		log.Debug("expose health API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	return mux
}
