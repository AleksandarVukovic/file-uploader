package api

import (
	"context"
	"errors"
	"io"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
)

type filesHandler struct {
	storage storage.Service
}

func NewFilesHandler(storage storage.Service) files.Service {
	return &filesHandler{storage: storage}
}

func (h *filesHandler) Upload(ctx context.Context, p *files.UploadPayload, body io.ReadCloser) error {
	log := logger.FromCtx(ctx)
	defer body.Close()

	res, err := h.storage.Upload(ctx, storage.UploadInput{
		Filename:    p.Filename,
		ContentType: p.ContentType,
		Size:        p.Size,
		Checksum:    p.Checksum,
		Body:        body,
	})
	if errors.Is(err, storage.ErrInvalidChecksum) {
		log.Error("upload rejected: malformed checksum", "filename", p.Filename, "checksum", p.Checksum)
		return files.MakeBadRequest(errors.New("Checksum must be a hex-encoded SHA-256 digest"))
	}
	if err != nil {
		log.Error("failed to store file", "err", err, "filename", p.Filename)
		return files.MakeInternalError(errors.New("Error while storing file"))
	}

	log.Info("file stored", "uuid", res.UUID, "filename", res.Filename, "contentType", res.ContentType, "checksum", p.Checksum, "size", res.Size)
	return nil
}
