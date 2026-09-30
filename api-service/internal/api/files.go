package api

import (
	"context"
	"errors"
	"io"

	goaApi "github.com/aleksandarv/file-uploader/api-service/gen/files"
	"github.com/aleksandarv/file-uploader/common/logger"
	goafs "github.com/aleksandarv/file-uploader/file-service/gen/files"
	goa "goa.design/goa/v3/pkg"
)

const maxUploadSize = 10 << 20 // 10Mb

type filesSvc struct {
	fsClient goafs.Service
}

func NewFilesSvc(fileServiceClient goafs.Service) goaApi.Service {
	return &filesSvc{fsClient: fileServiceClient}
}

func (s *filesSvc) Upload(ctx context.Context, p *goaApi.UploadPayload, body io.ReadCloser) error {
	log := logger.FromCtx(ctx)
	defer body.Close()

	// ensure that we don't accept body bigger than maxUploadSize
	lbody := io.NopCloser(io.LimitReader(body, maxUploadSize))
	err := s.fsClient.Upload(ctx, &goafs.UploadPayload{
		Filename:    p.Filename,
		Size:        p.Size,
		ContentType: p.ContentType,
		Checksum:    p.Checksum,
	}, lbody)

	if err != nil {
		var svcErr *goa.ServiceError
		if errors.As(err, &svcErr) && svcErr.Name == "bad_request" {
			log.Error("upload rejected by file-service", "filename", p.Filename, "err", err)
			return goaApi.MakeBadRequest(errors.New(svcErr.Message))
		}
		log.Error("failed to forward upload to file-service", "filename", p.Filename, "err", err)
		return goaApi.MakeInternalError(errors.New("Error while storing file"))
	}

	log.Info("file uploaded successfully", "filename", p.Filename)
	return nil
}
