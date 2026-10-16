package outbox

import (
	"context"
	"errors"
	"testing"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
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

func (m *mockQuerier) GetUnpublishedEvents(ctx context.Context, arg sqlc.GetUnpublishedEventsParams) ([]sqlc.OutboxEvent, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]sqlc.OutboxEvent), args.Error(1)
}

func (m *mockQuerier) MarkEventPublished(ctx context.Context, id pgtype.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

var eventID = uuid.MustParse("0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11")

func domainEvent() outbox.Event {
	return outbox.Event{
		ID:            eventID.String(),
		AggregateType: events.AggregateTypeFile,
		AggregateID:   "agg-1",
		EventType:     events.EventTypeFileUploaded,
		Payload:       []byte(`{"a":1}`),
	}
}

func unpublishedParams(limit int32) sqlc.GetUnpublishedEventsParams {
	return sqlc.GetUnpublishedEventsParams{AggregateType: events.AggregateTypeFile, Limit: limit}
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

func TestRepository_GetUnpublished(t *testing.T) {
	q := new(mockQuerier)
	q.On("GetUnpublishedEvents", mock.Anything, unpublishedParams(50)).Return([]sqlc.OutboxEvent{{
		ID:            pgtype.UUID{Bytes: eventID, Valid: true},
		AggregateType: events.AggregateTypeFile,
		AggregateID:   "agg-1",
		EventType:     events.EventTypeFileUploaded,
		Payload:       []byte(`{"a":1}`),
	}}, nil)

	got, err := New(q).GetUnpublished(context.Background(), events.AggregateTypeFile, 50)

	require.NoError(t, err)
	require.Equal(t, []outbox.Event{domainEvent()}, got)
	q.AssertExpectations(t)
}

func TestRepository_GetUnpublished_Empty(t *testing.T) {
	q := new(mockQuerier)
	q.On("GetUnpublishedEvents", mock.Anything, unpublishedParams(50)).Return([]sqlc.OutboxEvent{}, nil)

	got, err := New(q).GetUnpublished(context.Background(), events.AggregateTypeFile, 50)

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepository_GetUnpublished_QuerierError(t *testing.T) {
	boom := errors.New("db down")
	q := new(mockQuerier)
	q.On("GetUnpublishedEvents", mock.Anything, unpublishedParams(50)).Return([]sqlc.OutboxEvent{}, boom)

	got, err := New(q).GetUnpublished(context.Background(), events.AggregateTypeFile, 50)

	require.ErrorIs(t, err, boom)
	require.Nil(t, got)
}

func TestRepository_MarkPublished(t *testing.T) {
	q := new(mockQuerier)
	q.On("MarkEventPublished", mock.Anything, pgtype.UUID{Bytes: eventID, Valid: true}).Return(nil)

	err := New(q).MarkPublished(context.Background(), eventID.String())

	require.NoError(t, err)
	q.AssertExpectations(t)
}

func TestRepository_MarkPublished_InvalidID(t *testing.T) {
	q := new(mockQuerier)

	err := New(q).MarkPublished(context.Background(), "not-a-uuid")

	require.Error(t, err)
	q.AssertNotCalled(t, "MarkEventPublished", mock.Anything, mock.Anything)
}

func TestRepository_MarkPublished_QuerierError(t *testing.T) {
	boom := errors.New("db down")
	q := new(mockQuerier)
	q.On("MarkEventPublished", mock.Anything, mock.Anything).Return(boom)

	err := New(q).MarkPublished(context.Background(), eventID.String())

	require.ErrorIs(t, err, boom)
}
