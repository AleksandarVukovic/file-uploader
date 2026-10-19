package importer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

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
		addMetricMessage(context.Background(), outcomeUnknownType)
		i.log.Error("unknown event type, discarding", "messageId", d.MessageId, "type", d.Type)
		return rabbitmq.NackDiscard
	}

	var payload events.UsersImportedPayload
	if err := json.Unmarshal(d.Body, &payload); err != nil {
		addMetricMessage(context.Background(), outcomeInvalidPayload)
		i.log.Error("invalid event payload, discarding", "messageId", d.MessageId, "err", err)
		return rabbitmq.NackDiscard
	}

	ctx, span := tracer.Start(context.Background(), "import users")
	defer span.End()
	
	telemetry.SetFileID(ctx, payload.FileID)
	traceID := telemetry.TraceID(ctx)

	start := time.Now()
	var imported, duplicate, reserved int
	for _, u := range payload.Users {
		_, err := i.users.Import(ctx, u.Username, u.Email, u.FirstName, u.LastName)
		switch {
		case err == nil:
			imported++
		case errors.Is(err, users.ErrUsernameTaken), errors.Is(err, users.ErrEmailTaken):
			i.log.Info("user already exists, skipping", "messageId", d.MessageId, "fileId", payload.FileID, "trace_id", traceID, "username", u.Username)
			duplicate++
		case errors.Is(err, users.ErrReservedUsername):
			i.log.Error("username is reserved, skipping", "messageId", d.MessageId, "fileId", payload.FileID, "trace_id", traceID, "username", u.Username)
			reserved++
		default:
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			i.log.Error("failed to import user, requeueing", "messageId", d.MessageId, "fileId", payload.FileID, "trace_id", traceID, "batch", payload.Batch, "username", u.Username, "err", err)
			recordImportMetrics(ctx, start, imported, duplicate, reserved, outcomeRequeued)
			return rabbitmq.NackRequeue
		}
	}

	recordImportMetrics(ctx, start, imported, duplicate, reserved, outcomeProcessed)
	i.log.Info("users imported", "messageId", d.MessageId, "fileId", payload.FileID, "trace_id", traceID, "batch", payload.Batch, "imported", imported, "duplicate", duplicate, "reserved", reserved)
	return rabbitmq.Ack
}

func recordImportMetrics(ctx context.Context, start time.Time, imported, duplicate, reserved int, outcome string) {
	metricDuration.Record(ctx, time.Since(start).Seconds())
	addMetricRecords(ctx, recordOutcomeImported, int64(imported))
	addMetricRecords(ctx, recordOutcomeDuplicate, int64(duplicate))
	addMetricRecords(ctx, recordOutcomeReserved, int64(reserved))
	addMetricMessage(ctx, outcome)
}
