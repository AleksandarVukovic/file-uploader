package processor

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/messaging"
	"github.com/wagslane/go-rabbitmq"
)

const publishBatchSize = 50

var (
	ErrObjectNotFound   = errors.New("object not found")
	ErrBucketNotAllowed = errors.New("bucket not allowed")
	ErrInvalidHeader    = errors.New("invalid csv header")
)

var (
	csvHeader     = []string{"id", "username", "name", "email"}
	usernameRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

type ObjectStore interface {
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

type Processor struct {
	log       *slog.Logger
	store     ObjectStore
	publisher messaging.Publisher
}

func New(log *slog.Logger, store ObjectStore, publisher messaging.Publisher) *Processor {
	return &Processor{log: log, store: store, publisher: publisher}
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
	case errors.Is(err, ErrObjectNotFound), errors.Is(err, ErrBucketNotAllowed), errors.Is(err, ErrInvalidHeader):
		log.Error("file cannot be processed, discarding", "fileId", payload.FileID, "err", err)
		return rabbitmq.NackDiscard
	default:
		log.Error("failed to process file, requeueing", "fileId", payload.FileID, "err", err)
		return rabbitmq.NackRequeue
	}
}

func (p *Processor) processFileUploaded(ctx context.Context, payload events.FileUploadedPayload) error {
	log := logger.FromCtx(ctx)

	body, err := p.store.Get(ctx, payload.RootDir, payload.Path)
	if err != nil {
		return err
	}
	defer body.Close()

	reader := csv.NewReader(body)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true

	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("object %q: %w: file is empty", payload.Path, ErrInvalidHeader)
		}
		return fmt.Errorf("read object %q: %w", payload.Path, err)
	}
	if !slices.Equal(header, csvHeader) {
		return fmt.Errorf("object %q: %w: expected %v, got %v", payload.Path, ErrInvalidHeader, csvHeader, header)
	}

	valid, skipped, batches := 0, 0, 0
	batch := make([]events.UserPayload, 0, publishBatchSize)
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := reader.FieldPos(0)

		if _, ok := errors.AsType[*csv.ParseError](err); ok {
			log.Error("skipping malformed row", "fileId", payload.FileID, "line", line, "err", err)
			skipped++
			continue
		}
		if err != nil {
			return fmt.Errorf("read object %q: %w", payload.Path, err)
		}

		user, err := parseUser(row)
		if err != nil {
			log.Error("skipping invalid row", "fileId", payload.FileID, "line", line, "err", err)
			skipped++
			continue
		}
		valid++
		batch = append(batch, user)

		if len(batch) == publishBatchSize {
			if err := p.publishBatch(ctx, payload.FileID, batches, batch); err != nil {
				return fmt.Errorf("publish users from object %q: %w", payload.Path, err)
			}
			batches++
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		if err := p.publishBatch(ctx, payload.FileID, batches, batch); err != nil {
			return fmt.Errorf("publish users from object %q: %w", payload.Path, err)
		}
		batches++
	}

	log.Info("csv processed", "fileId", payload.FileID, "valid", valid, "skipped", skipped, "batches", batches)
	return nil
}

func (p *Processor) publishBatch(ctx context.Context, fileID string, batch int, users []events.UserPayload) error {
	body, err := json.Marshal(events.UsersImportedPayload{FileID: fileID, Batch: batch, Users: users})
	if err != nil {
		return fmt.Errorf("marshal users batch %d: %w", batch, err)
	}

	return p.publisher.Publish(ctx, messaging.Message{
		ID:         fmt.Sprintf("%s-%d", fileID, batch),
		Type:       events.EventTypeUsersImported,
		RoutingKey: events.RoutingKeyUsersImported,
		Body:       body,
	})
}

func parseUser(row []string) (events.UserPayload, error) {
	if len(row) != len(csvHeader) {
		return events.UserPayload{}, fmt.Errorf("expected %d fields, got %d", len(csvHeader), len(row))
	}

	id, err := strconv.ParseInt(strings.TrimSpace(row[0]), 10, 64)
	if err != nil || id <= 0 {
		return events.UserPayload{}, fmt.Errorf("id %q must be a positive integer", row[0])
	}

	if !usernameRegex.MatchString(row[1]) {
		return events.UserPayload{}, fmt.Errorf("invalid username %q", row[1])
	}

	firstName, lastName, _ := strings.Cut(strings.TrimSpace(row[2]), " ")
	if strings.Contains(lastName, " ") {
		return events.UserPayload{}, errors.New("invalid full name")
	}
	if firstName == "" {
		return events.UserPayload{}, errors.New("firstName must not be empty")
	}
	if lastName == "" {
		return events.UserPayload{}, errors.New("lastName must not be empty")
	}

	email := strings.TrimSpace(row[3])
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return events.UserPayload{}, fmt.Errorf("email %q is not a valid address", row[3])
	}

	return events.UserPayload{
		ID:        id,
		Username:  row[1],
		FirstName: firstName,
		LastName:  lastName,
		Email:     strings.ToLower(email),
	}, nil
}
