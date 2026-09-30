package api

import (
	"context"

	"github.com/aleksandarv/file-uploader/file-service/gen/health"
)

type healthsvc struct {
}

func NewHealthSvc() health.Service {
	return &healthsvc{}
}

func (s healthsvc) Health(context.Context) (res *health.HealthResult, err error) {
	return &health.HealthResult{
		Status: "ok",
	}, nil
}
