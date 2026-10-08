package api

import (
	"context"

	"github.com/aleksandarv/file-uploader/api-service/gen/health"
)

type healthHandler struct {
}

func NewHealthHandler() health.Service {
	return &healthHandler{}
}

func (h healthHandler) Health(context.Context) (res *health.HealthResult, err error) {
	return &health.HealthResult{
		Status: "ok",
	}, nil
}
