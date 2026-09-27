package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/aleksandarv/file-uploader/api-service/gen/files"
	"github.com/aleksandarv/file-uploader/common/logger"
)

const uploadDir = "uploads"

type filesService struct{}

func NewFilesService() files.Service {
	return &filesService{}
}

func (s *filesService) Upload(ctx context.Context, p *files.UploadPayload, body io.ReadCloser) error {
	log := logger.FromCtx(ctx)
	defer body.Close()

	// TODO: temp
	safeName := filepath.Base(p.Filename)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		log.Error("failed to prepare upload directory", "err", err)
		return files.MakeInternalError(errors.New("Error while storing file"))
	}

	f, err := os.Create(filepath.Join(uploadDir, safeName))
	if err != nil {
		log.Error("failed to create file for upload", "err", err, "filename", safeName)
		return files.MakeInternalError(errors.New("Error while storing file"))
	}
	defer f.Close()

	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(hasher, f), io.LimitReader(body, p.Size+1))
	if err != nil {
		log.Error("failed to read upload body", "err", err, "filename", p.Filename)
		return files.MakeInternalError(errors.New("Error while processing uploaded file"))
	}
	if n != p.Size {
		log.Error("upload rejected: size mismatch", "filename", p.Filename, "declaredSize", p.Size, "receivedSize", n)
		return files.MakeBadRequest(errors.New("File size does not match declared size"))
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != p.Checksum {
		log.Error("upload rejected: checksum mismatch", "filename", p.Filename, "expected", p.Checksum, "got", checksum)
		return files.MakeBadRequest(errors.New("Uploaded file checksum does not match declared checksum"))
	}

	log.Info("file uploaded successfully", "filename", p.Filename)
	return nil
}
