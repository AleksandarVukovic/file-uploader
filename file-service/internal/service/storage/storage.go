package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidChecksum = errors.New("checksum must be a hex-encoded SHA-256 digest")

type StoreInput struct {
	UserID      int64
	Filename    string
	ContentType string
	Size        int64
	Checksum    string // hex-encoded SHA-256
	Body        io.Reader
}

type StoreResult struct {
	UUID        string
	Filename    string
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

type Service interface {
	Store(ctx context.Context, in StoreInput) (StoreResult, error)
}

type PutInput struct {
	Key            string
	Body           io.Reader
	Size           int64
	ContentType    string
	ChecksumSHA256 []byte
}

type ObjectStore interface {
	Put(ctx context.Context, in PutInput) error
}

type File struct {
	ID             string // UUID
	UserID         int64
	Filename       string
	ContentType    string
	Path           string
	RootDir        string
	Size           int64
	ChecksumSHA256 []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Repository interface {
	Insert(ctx context.Context, file File) (File, error)
}

type service struct {
	objectStore ObjectStore
	repo        Repository
	rootDir     string
}

func New(objectStore ObjectStore, repo Repository, rootDir string) Service {
	return &service{objectStore: objectStore, repo: repo, rootDir: rootDir}
}

func (s *service) Store(ctx context.Context, in StoreInput) (StoreResult, error) {
	checksum, err := hex.DecodeString(in.Checksum)
	if err != nil {
		return StoreResult{}, ErrInvalidChecksum
	}

	id := uuid.NewString()
	key := strconv.FormatInt(in.UserID, 10) + "/" + id

	err = s.objectStore.Put(ctx, PutInput{
		Key:            key,
		Body:           in.Body,
		Size:           in.Size,
		ContentType:    in.ContentType,
		ChecksumSHA256: checksum,
	})
	if err != nil {
		return StoreResult{}, err
	}

	file, err := s.repo.Insert(ctx, File{
		ID:             id,
		UserID:         in.UserID,
		Filename:       in.Filename,
		ContentType:    in.ContentType,
		Path:           key,
		RootDir:        s.rootDir,
		Size:           in.Size,
		ChecksumSHA256: checksum,
	})
	if err != nil {
		return StoreResult{}, err
	}

	return StoreResult{
		UUID:        id,
		Filename:    in.Filename,
		ContentType: in.ContentType,
		Size:        in.Size,
		CreatedAt:   file.CreatedAt,
	}, nil
}
