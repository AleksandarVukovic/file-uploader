package broker

import (
	"context"
	"errors"
	"testing"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/messaging"
	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testBatchSize = 25

type mockOutbox struct {
	mock.Mock
}

func (m *mockOutbox) Insert(ctx context.Context, event outbox.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockOutbox) GetUnpublished(ctx context.Context, aggregateType string, limit int) ([]outbox.Event, error) {
	args := m.Called(ctx, aggregateType, limit)
	return args.Get(0).([]outbox.Event), args.Error(1)
}

func (m *mockOutbox) MarkPublished(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

type mockPublisher struct {
	mock.Mock
}

func (m *mockPublisher) Publish(ctx context.Context, msg messaging.Message) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

func testCtx() context.Context {
	return logger.WithCtx(context.Background(), logger.NewLogger(false))
}

func fileEvent(id string) outbox.Event {
	return outbox.Event{
		ID:            id,
		AggregateType: events.AggregateTypeFile,
		AggregateID:   "file-" + id,
		EventType:     events.EventTypeFileUploaded,
		Payload:       []byte(`{"fileId":"file-` + id + `"}`),
	}
}

func expectedMessage(id string) messaging.Message {
	return messaging.Message{
		ID:         id,
		Type:       events.EventTypeFileUploaded,
		RoutingKey: "file.uploaded",
		Body:       []byte(`{"fileId":"file-` + id + `"}`),
	}
}

func newTestService(repo outbox.Repository, publisher messaging.Publisher) Service {
	return New(repo, publisher, testBatchSize)
}

func TestService_PublishFileEvents_PublishesAndMarksEachEvent(t *testing.T) {
	var calls []string
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).
		Return([]outbox.Event{fileEvent("a"), fileEvent("b")}, nil)
	repo.On("MarkPublished", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { calls = append(calls, "Mark:"+args.String(1)) }).
		Return(nil)
	publisher := new(mockPublisher)
	publisher.On("Publish", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { calls = append(calls, "Publish:"+args.Get(1).(messaging.Message).ID) }).
		Return(nil)

	err := newTestService(repo, publisher).PublishFileEvents(testCtx())

	require.NoError(t, err)
	require.Equal(t, []string{"Publish:a", "Mark:a", "Publish:b", "Mark:b"}, calls)
	publisher.AssertCalled(t, "Publish", mock.Anything, expectedMessage("a"))
	publisher.AssertCalled(t, "Publish", mock.Anything, expectedMessage("b"))
	repo.AssertExpectations(t)
	publisher.AssertExpectations(t)
}

func TestService_PublishFileEvents_NoEvents(t *testing.T) {
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).Return([]outbox.Event{}, nil)
	publisher := new(mockPublisher)

	err := newTestService(repo, publisher).PublishFileEvents(testCtx())

	require.NoError(t, err)
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "MarkPublished", mock.Anything, mock.Anything)
}

func TestService_PublishFileEvents_FetchErrorIsReturned(t *testing.T) {
	boom := errors.New("db down")
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).Return([]outbox.Event{}, boom)
	publisher := new(mockPublisher)

	err := newTestService(repo, publisher).PublishFileEvents(testCtx())

	require.ErrorIs(t, err, boom)
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "MarkPublished", mock.Anything, mock.Anything)
}

func TestService_PublishFileEvents_PublishErrorSkipsMarkAndContinues(t *testing.T) {
	boom := errors.New("broker unreachable")
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).
		Return([]outbox.Event{fileEvent("a"), fileEvent("b")}, nil)
	repo.On("MarkPublished", mock.Anything, "b").Return(nil)
	publisher := new(mockPublisher)
	publisher.On("Publish", mock.Anything, expectedMessage("a")).Return(boom)
	publisher.On("Publish", mock.Anything, expectedMessage("b")).Return(nil)

	err := newTestService(repo, publisher).PublishFileEvents(testCtx())

	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, "a")
	repo.AssertNotCalled(t, "MarkPublished", mock.Anything, "a")
	repo.AssertExpectations(t)
	publisher.AssertExpectations(t)
}

func TestService_PublishFileEvents_MarkErrorIsReturnedAndContinues(t *testing.T) {
	boom := errors.New("db down")
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).
		Return([]outbox.Event{fileEvent("a"), fileEvent("b")}, nil)
	repo.On("MarkPublished", mock.Anything, "a").Return(boom)
	repo.On("MarkPublished", mock.Anything, "b").Return(nil)
	publisher := new(mockPublisher)
	publisher.On("Publish", mock.Anything, mock.Anything).Return(nil)

	err := newTestService(repo, publisher).PublishFileEvents(testCtx())

	require.ErrorIs(t, err, boom)
	repo.AssertExpectations(t)
	publisher.AssertNumberOfCalls(t, "Publish", 2)
}

func TestService_PublishFileEvents_UnknownEventTypeIsNotPublished(t *testing.T) {
	unknown := fileEvent("a")
	unknown.EventType = "FileDeleted"
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).
		Return([]outbox.Event{unknown, fileEvent("b")}, nil)
	repo.On("MarkPublished", mock.Anything, "b").Return(nil)
	publisher := new(mockPublisher)
	publisher.On("Publish", mock.Anything, expectedMessage("b")).Return(nil)

	err := newTestService(repo, publisher).PublishFileEvents(testCtx())

	require.ErrorContains(t, err, "FileDeleted")
	publisher.AssertNumberOfCalls(t, "Publish", 1)
	repo.AssertNotCalled(t, "MarkPublished", mock.Anything, "a")
	repo.AssertExpectations(t)
}

func TestService_PublishFileEvents_CancelledContextStopsPublishing(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx())
	cancel()
	repo := new(mockOutbox)
	repo.On("GetUnpublished", mock.Anything, events.AggregateTypeFile, testBatchSize).
		Return([]outbox.Event{fileEvent("a")}, nil)
	publisher := new(mockPublisher)

	err := newTestService(repo, publisher).PublishFileEvents(ctx)

	require.ErrorIs(t, err, context.Canceled)
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "MarkPublished", mock.Anything, mock.Anything)
}
