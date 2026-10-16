package api

import (
	"log/slog"
	"net/http"

	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/aleksandarv/file-uploader/file-processing-service/gen/health"
	healthsvr "github.com/aleksandarv/file-uploader/file-processing-service/gen/http/health/server"
	goahttp "goa.design/goa/v3/http"
)

func HealthRoutes(log *slog.Logger, healthHandler health.Service) http.Handler {
	mux := goahttp.NewMuxer()

	healthSrv := healthsvr.New(health.NewEndpoints(healthHandler), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	healthSrv.Use(httpmiddleware.PanicHandler())
	healthSrv.Use(httpmiddleware.Logger(log, false))
	healthSrv.Mount(mux)
	for _, m := range healthSrv.Mounts {
		log.Debug("expose health API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	return mux
}
