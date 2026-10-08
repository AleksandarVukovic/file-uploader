package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strconv"

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

type service struct {
	objectStore ObjectStore
}

func New(objectStore ObjectStore) Service {
	return &service{objectStore: objectStore}
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

	return StoreResult{
		UUID:        id,
		Filename:    in.Filename,
		ContentType: in.ContentType,
		Size:        in.Size,
	}, nil
}
