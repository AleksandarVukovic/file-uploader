package storage

import (
	"context"
	"errors"
	"io"
)

var ErrInvalidChecksum = errors.New("checksum must be a hex-encoded SHA-256 digest")

type UploadInput struct {
	UserID      int64
	UUID        string
	Filename    string
	ContentType string
	Size        int64
	Checksum    string // SHA-256
	Body        io.Reader
}

type UploadResult struct {
	UUID        string
	Filename    string
	ContentType string
	Size        int64
}

type Service interface {
	Upload(ctx context.Context, in UploadInput) (UploadResult, error)
}
