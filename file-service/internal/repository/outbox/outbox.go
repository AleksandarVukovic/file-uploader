package outbox

import (
	"context"
	"fmt"

	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type repository struct {
	queries sqlc.Querier
}

func New(queries sqlc.Querier) storage.OutboxRepository {
	return &repository{queries: queries}
}

func (r *repository) Insert(ctx context.Context, event storage.OutboxEvent) error {
	id, err := uuid.Parse(event.ID)
	if err != nil {
		return fmt.Errorf("invalid outbox event id %q: %w", event.ID, err)
	}

	_, err = r.queries.InsertEvent(ctx, sqlc.InsertEventParams{
		ID:            pgtype.UUID{Bytes: id, Valid: true},
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		EventType:     event.EventType,
		Payload:       event.Payload,
	})
	return err
}
