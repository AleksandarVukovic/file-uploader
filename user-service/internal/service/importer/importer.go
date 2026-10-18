package importer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/wagslane/go-rabbitmq"
)

type userService interface {
	Import(ctx context.Context, username, email, firstName, lastName string) (users.User, error)
}

type Importer struct {
	log   *slog.Logger
	users userService
}

func New(log *slog.Logger, users userService) *Importer {
	return &Importer{log: log, users: users}
}

func (i *Importer) Process(d rabbitmq.Delivery) rabbitmq.Action {
	ctx := logger.WithCtx(context.Background(), i.log.With("messageId", d.MessageId))
	log := logger.FromCtx(ctx)

	if d.Type != events.EventTypeUsersImported {
		log.Error("unknown event type, discarding", "type", d.Type)
		return rabbitmq.NackDiscard
	}

	var payload events.UsersImportedPayload
	if err := json.Unmarshal(d.Body, &payload); err != nil {
		log.Error("invalid event payload, discarding", "err", err)
		return rabbitmq.NackDiscard
	}

	var imported, skipped int
	for _, u := range payload.Users {
		_, err := i.users.Import(ctx, u.Username, u.Email, u.FirstName, u.LastName)
		switch {
		case err == nil:
			imported++
		case errors.Is(err, users.ErrUsernameTaken), errors.Is(err, users.ErrEmailTaken):
			log.Info("user already exists, skipping", "fileId", payload.FileID, "username", u.Username)
			skipped++
		case errors.Is(err, users.ErrReservedUsername):
			log.Error("username is reserved, skipping", "fileId", payload.FileID, "username", u.Username)
			skipped++
		default:
			log.Error("failed to import user, requeueing", "fileId", payload.FileID, "batch", payload.Batch, "username", u.Username, "err", err)
			return rabbitmq.NackRequeue
		}
	}

	log.Info("users imported", "fileId", payload.FileID, "batch", payload.Batch, "imported", imported, "skipped", skipped)
	return rabbitmq.Ack
}
