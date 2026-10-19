package storage

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/telemetry"
	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
	"github.com/google/uuid"
)

const (
	cleanupTimeout   = 5 * time.Second
	pendingBatchSize = 100
)

var (
	ErrInvalidChecksum  = errors.New("checksum must be a hex-encoded SHA-256 digest")
	ErrChecksumMismatch = errors.New("checksum does not match the uploaded content")
	ErrIncompleteBody   = errors.New("uploaded content is shorter than the declared size")
	ErrDuplicateFile    = errors.New("file has been already uploaded")
)

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
	Exists(ctx context.Context, key string) (bool, error)
}

type Status string

const (
	// indicates that file uploading is in progress
	StatusPending  Status = "pending"
	StatusUploaded Status = "uploaded"
	// indicates that file processing has failed
	StatusFailed Status = "failed"
)

type File struct {
	ID             string // UUID
	UserID         int64
	Filename       string
	ContentType    string
	Path           string
	RootDir        string
	Size           int64
	ChecksumSHA256 []byte
	Status         Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Repository interface {
	Insert(ctx context.Context, file File) (File, error)
	UpdateStatus(ctx context.Context, id string, status Status) (File, error)
	Delete(ctx context.Context, id string) error
	GetPendingFilesOlderThan(ctx context.Context, after, before time.Time) ([]File, error)
}

type Transactor interface {
	WithinTx(ctx context.Context, txFunc func(txCtx context.Context) error) error
}

type service struct {
	objectStore    ObjectStore
	repo           Repository
	outboxRepo     outbox.Repository
	tx             Transactor
	rootDir        string
	pendingFileTTL time.Duration
}

func New(objectStore ObjectStore, repo Repository, outboxRepo outbox.Repository, tx Transactor, rootDir string, pendingFileTTL time.Duration) Service {
	return &service{objectStore: objectStore, repo: repo, outboxRepo: outboxRepo, tx: tx, rootDir: rootDir, pendingFileTTL: pendingFileTTL}
}

func (s *service) Store(ctx context.Context, in StoreInput) (StoreResult, error) {
	log := logger.FromCtx(ctx)

	checksum, err := hex.DecodeString(in.Checksum)
	if err != nil {
		return StoreResult{}, ErrInvalidChecksum
	}

	id := uuid.NewString()
	key := strconv.FormatInt(in.UserID, 10) + "/" + id

	telemetry.SetFileID(ctx, id)

	file, err := s.repo.Insert(ctx, File{
		ID:             id,
		UserID:         in.UserID,
		Filename:       in.Filename,
		ContentType:    in.ContentType,
		Path:           key,
		RootDir:        s.rootDir,
		Size:           in.Size,
		ChecksumSHA256: checksum,
		Status:         StatusPending,
	})
	if err != nil {
		return StoreResult{}, err
	}
	log.Info("file record successfully instered in db", "fileId", id, "status", file.Status)

	err = s.objectStore.Put(ctx, PutInput{
		Key:            key,
		Body:           in.Body,
		Size:           in.Size,
		ContentType:    in.ContentType,
		ChecksumSHA256: checksum,
	})
	if err != nil {
		log.Error("failed to save file to object store", "fileId", id, "err", err)
		// detach cleanup task from request
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if delErr := s.repo.Delete(cleanupCtx, id); delErr != nil {
			log.Error("failed to delete file record after failed upload", "fileId", id, "err", delErr)
		}
		return StoreResult{}, err
	}
	log.Info("file successfully saved to object store", "fileId", id)

	file, err = s.markUploaded(ctx, id, file)
	if err != nil {
		log.Error("failed to mark file as uploaded", "fileId", id, "err", err)
		return StoreResult{}, err
	}
	log.Info("file status updated to uploaded and outbox event recorded", "fileId", id, "status", file.Status)

	return StoreResult{
		UUID:        id,
		Filename:    in.Filename,
		ContentType: in.ContentType,
		Size:        in.Size,
		CreatedAt:   file.CreatedAt,
	}, nil
}

func (s *service) markUploaded(ctx context.Context, id string, file File) (File, error) {
	event, err := newFileUploadedEvent(file)
	if err != nil {
		return File{}, err
	}

	var updated File
	err = s.tx.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.repo.UpdateStatus(txCtx, id, StatusUploaded)
		if err != nil {
			return err
		}
		return s.outboxRepo.Insert(txCtx, event)
	})
	if err != nil {
		return File{}, err
	}
	return updated, nil
}

func newFileUploadedEvent(file File) (outbox.Event, error) {
	payload, err := json.Marshal(events.FileUploadedPayload{
		FileID:      file.ID,
		UserID:      file.UserID,
		RootDir:     file.RootDir,
		Path:        file.Path,
		ContentType: file.ContentType,
		Size:        file.Size,
		Checksum:    hex.EncodeToString(file.ChecksumSHA256),
	})
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{
		ID:            uuid.NewString(),
		AggregateType: events.AggregateTypeFile,
		AggregateID:   file.ID,
		EventType:     events.EventTypeFileUploaded,
		Payload:       payload,
	}, nil
}

func (s *service) CleanupPendingFiles(ctx context.Context) error {
	log := logger.FromCtx(ctx)
	log.Info("starting cleanup of pending files older than", "seconds", s.pendingFileTTL)

	olderThan := time.Now().Add(-s.pendingFileTTL)
	deleted := 0
	updated := 0
	failed := 0
	after := time.Time{}
	for {
		files, err := s.repo.GetPendingFilesOlderThan(ctx, after, olderThan)
		if err != nil {
			return err
		}

		for _, f := range files {
			exists, err := s.objectStore.Exists(ctx, f.Path)
			if err != nil {
				log.Error("failed to check if file exists in object store", "fileId", f.ID, "path", f.Path, "err", err)
				failed++
				continue
			}
			if exists {
				_, err = s.markUploaded(ctx, f.ID, f)
				if err != nil {
					log.Error("failed to mark pending file which exists in object store as uploaded", "fileId", f.ID, "err", err)
					failed++
					continue
				}
				updated++
			} else {
				err = s.repo.Delete(ctx, f.ID)
				if err != nil {
					log.Error("failed to delete pending file record", "fileId", f.ID, "err", err)
					failed++
					continue
				}
				deleted++
			}
		}

		// it means that last batch was handled and cleanup is done
		if len(files) < pendingBatchSize {
			break
		}
		// moving cursor to latest CreatedAt
		after = files[len(files)-1].CreatedAt
	}

	log.Info("cleanup of pending files completed", "updated", updated, "deleted", deleted, "failedToApplyCleanup", failed, "olderThan", olderThan)
	return nil
}
