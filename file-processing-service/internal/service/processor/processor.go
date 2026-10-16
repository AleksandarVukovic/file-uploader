package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/wagslane/go-rabbitmq"
)

var (
	ErrObjectNotFound   = errors.New("object not found")
	ErrBucketNotAllowed = errors.New("bucket not allowed")
	ErrChecksumMismatch = errors.New("checksum mismatch")
)

type ObjectStore interface {
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

type Processor struct {
	log   *slog.Logger
	store ObjectStore
}

func New(log *slog.Logger, store ObjectStore) *Processor {
	return &Processor{log: log, store: store}
}

func (p *Processor) Process(d rabbitmq.Delivery) rabbitmq.Action {
	ctx := logger.WithCtx(context.Background(), p.log.With("messageId", d.MessageId))
	log := logger.FromCtx(ctx)

	if d.Type != events.EventTypeFileUploaded {
		log.Error("unknown event type, discarding", "type", d.Type)
		return rabbitmq.NackDiscard
	}

	var payload events.FileUploadedPayload
	if err := json.Unmarshal(d.Body, &payload); err != nil {
		log.Error("invalid event payload, discarding", "err", err)
		return rabbitmq.NackDiscard
	}

	err := p.processFileUploaded(ctx, payload)
	switch {
	case err == nil:
		log.Info("file processed", "fileId", payload.FileID)
		return rabbitmq.Ack
	case errors.Is(err, ErrObjectNotFound), errors.Is(err, ErrBucketNotAllowed), errors.Is(err, ErrChecksumMismatch):
		log.Error("file cannot be processed, discarding", "fileId", payload.FileID, "err", err)
		return rabbitmq.NackDiscard
	default:
		log.Error("failed to process file, requeueing", "fileId", payload.FileID, "err", err)
		return rabbitmq.NackRequeue
	}
}

func (p *Processor) processFileUploaded(ctx context.Context, payload events.FileUploadedPayload) error {
	body, err := p.store.Get(ctx, payload.RootDir, payload.Path)
	if err != nil {
		return err
	}
	defer body.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, body); err != nil {
		return fmt.Errorf("read object %q: %w", payload.Path, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != payload.Checksum {
		return fmt.Errorf("object %q: %w", payload.Path, ErrChecksumMismatch)
	}
	return nil
}
