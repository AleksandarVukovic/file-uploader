package api

import (
	"context"
	"errors"
	"io"

	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
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

func (h *filesHandler) Upload(ctx context.Context, p *files.UploadPayload, body io.ReadCloser) (*files.UploadResult, error) {
	log := logger.FromCtx(ctx)
	defer body.Close()

	userID, ok := httpmiddleware.UserIDFromCtx(ctx)
	if !ok {
		log.Error("upload rejected: missing user ID")
		return nil, files.MakeBadRequest(errors.New("User ID is required"))
	}

	res, err := h.storage.Store(ctx, storage.StoreInput{
		UserID:      userID,
		Filename:    p.Filename,
		ContentType: p.ContentType,
		Size:        p.Size,
		Checksum:    p.Checksum,
		Body:        body,
	})
	if errors.Is(err, storage.ErrInvalidChecksum) {
		log.Error("upload rejected: malformed checksum", "filename", p.Filename, "checksum", p.Checksum)
		return nil, files.MakeBadRequest(errors.New("Checksum must be a hex-encoded SHA-256 digest"))
	}
	if errors.Is(err, storage.ErrChecksumMismatch) {
		log.Error("upload rejected: checksum mismatch", "err", err, "filename", p.Filename, "checksum", p.Checksum)
		return nil, files.MakeBadRequest(errors.New("Checksum does not match the uploaded content"))
	}
	if errors.Is(err, storage.ErrIncompleteBody) {
		log.Error("upload rejected: incomplete body", "err", err, "filename", p.Filename, "size", p.Size)
		return nil, files.MakeBadRequest(errors.New("Uploaded content is shorter than the declared size"))
	}
	if err != nil {
		log.Error("failed to store file", "err", err, "filename", p.Filename)
		return nil, files.MakeInternalError(errors.New("Error while storing file"))
	}

	log.Info("file stored", "uuid", res.UUID, "filename", res.Filename, "contentType", res.ContentType, "checksum", p.Checksum, "size", res.Size)
	return &files.UploadResult{
		UUID:        res.UUID,
		Filename:    res.Filename,
		ContentType: res.ContentType,
		Size:        res.Size,
	}, nil
}
