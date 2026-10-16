package outbox

import (
	"context"
	"fmt"

	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type repository struct {
	queries sqlc.Querier
}

func New(queries sqlc.Querier) outbox.Repository {
	return &repository{queries: queries}
}

func (r *repository) Insert(ctx context.Context, event outbox.Event) error {
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

func (r *repository) GetUnpublished(ctx context.Context, aggregateType string, limit int) ([]outbox.Event, error) {
	rows, err := r.queries.GetUnpublishedEvents(ctx, sqlc.GetUnpublishedEventsParams{
		AggregateType: aggregateType,
		Limit:         int32(limit),
	})
	if err != nil {
		return nil, err
	}

	events := make([]outbox.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, outbox.Event{
			ID:            uuid.UUID(row.ID.Bytes).String(),
			AggregateType: row.AggregateType,
			AggregateID:   row.AggregateID,
			EventType:     row.EventType,
			Payload:       row.Payload,
		})
	}
	return events, nil
}

func (r *repository) MarkPublished(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid outbox event id %q: %w", id, err)
	}
	return r.queries.MarkEventPublished(ctx, pgtype.UUID{Bytes: parsed, Valid: true})
}
