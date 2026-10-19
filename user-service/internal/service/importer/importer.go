package importer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/telemetry"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/wagslane/go-rabbitmq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("user-service/importer")

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
	if d.Type != events.EventTypeUsersImported {
		i.log.Error("unknown event type, discarding", "messageId", d.MessageId, "type", d.Type)
		return rabbitmq.NackDiscard
	}

	var payload events.UsersImportedPayload
	if err := json.Unmarshal(d.Body, &payload); err != nil {
		i.log.Error("invalid event payload, discarding", "messageId", d.MessageId, "err", err)
		return rabbitmq.NackDiscard
	}

	ctx, span := tracer.Start(context.Background(), "import users")
	defer span.End()
	telemetry.SetFileID(ctx, payload.FileID)

	var imported, skipped int
	for _, u := range payload.Users {
		_, err := i.users.Import(ctx, u.Username, u.Email, u.FirstName, u.LastName)
		switch {
		case err == nil:
			imported++
		case errors.Is(err, users.ErrUsernameTaken), errors.Is(err, users.ErrEmailTaken):
			i.log.Info("user already exists, skipping", "messageId", d.MessageId, "fileId", payload.FileID, "username", u.Username)
			skipped++
		case errors.Is(err, users.ErrReservedUsername):
			i.log.Error("username is reserved, skipping", "messageId", d.MessageId, "fileId", payload.FileID, "username", u.Username)
			skipped++
		default:
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			i.log.Error("failed to import user, requeueing", "messageId", d.MessageId, "fileId", payload.FileID, "batch", payload.Batch, "username", u.Username, "err", err)
			return rabbitmq.NackRequeue
		}
	}

	i.log.Info("users imported", "messageId", d.MessageId, "fileId", payload.FileID, "batch", payload.Batch, "imported", imported, "skipped", skipped)
	return rabbitmq.Ack
}
