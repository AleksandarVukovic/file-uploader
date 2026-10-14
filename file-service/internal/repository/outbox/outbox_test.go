package outbox

import (
	"context"
	"errors"
	"testing"

	"github.com/aleksandarv/file-uploader/common/events"
	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockQuerier struct {
	mock.Mock
	sqlc.Querier
}

func (m *mockQuerier) InsertEvent(ctx context.Context, arg sqlc.InsertEventParams) (sqlc.OutboxEvent, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlc.OutboxEvent), args.Error(1)
}

var eventID = uuid.MustParse("0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11")

func domainEvent() storage.OutboxEvent {
	return storage.OutboxEvent{
		ID:            eventID.String(),
		AggregateType: events.AggregateTypeFile,
		AggregateID:   "agg-1",
		EventType:     events.EventTypeFileUploaded,
		Payload:       []byte(`{"a":1}`),
	}
}

func TestRepository_Insert(t *testing.T) {
	q := new(mockQuerier)
	q.On("InsertEvent", mock.Anything, sqlc.InsertEventParams{
		ID:            pgtype.UUID{Bytes: eventID, Valid: true},
		AggregateType: events.AggregateTypeFile,
		AggregateID:   "agg-1",
		EventType:     events.EventTypeFileUploaded,
		Payload:       []byte(`{"a":1}`),
	}).Return(sqlc.OutboxEvent{}, nil)

	err := New(q).Insert(context.Background(), domainEvent())

	require.NoError(t, err)
	q.AssertExpectations(t)
}

func TestRepository_Insert_InvalidID(t *testing.T) {
	q := new(mockQuerier)
	event := domainEvent()
	event.ID = "not-a-uuid"

	err := New(q).Insert(context.Background(), event)

	require.Error(t, err)
	q.AssertNotCalled(t, "InsertEvent", mock.Anything, mock.Anything)
}

func TestRepository_Insert_QuerierError(t *testing.T) {
	boom := errors.New("db down")
	q := new(mockQuerier)
	q.On("InsertEvent", mock.Anything, mock.Anything).Return(sqlc.OutboxEvent{}, boom)

	err := New(q).Insert(context.Background(), domainEvent())

	require.ErrorIs(t, err, boom)
}
