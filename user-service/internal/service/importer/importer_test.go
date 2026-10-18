package importer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wagslane/go-rabbitmq"
)

type mockUserService struct {
	mock.Mock
}

func (m *mockUserService) Import(ctx context.Context, username, email, firstName, lastName string) (users.User, error) {
	args := m.Called(ctx, username, email, firstName, lastName)
	return args.Get(0).(users.User), args.Error(1)
}

func userPayload(username string) events.UserPayload {
	return events.UserPayload{
		ID:        1,
		Username:  username,
		FirstName: "First",
		LastName:  "Last",
		Email:     username + "@example.com",
	}
}

func delivery(t *testing.T, eventType string, payload events.UsersImportedPayload) rabbitmq.Delivery {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return rabbitmq.Delivery{Delivery: amqp.Delivery{Type: eventType, MessageId: "file-1-0", Body: body}}
}

func expectImport(svc *mockUserService, u events.UserPayload, err error) {
	svc.On("Import", mock.Anything, u.Username, u.Email, u.FirstName, u.LastName).Return(users.User{}, err)
}

func TestImporter_Process(t *testing.T) {
	t.Run("acks and imports every user in the batch", func(t *testing.T) {
		svc := &mockUserService{}
		alice, bob := userPayload("alice"), userPayload("bob")
		expectImport(svc, alice, nil)
		expectImport(svc, bob, nil)
		d := delivery(t, events.EventTypeUsersImported, events.UsersImportedPayload{FileID: "file-1", Users: []events.UserPayload{alice, bob}})

		action := New(logger.NewLogger(false), svc).Process(d)

		require.Equal(t, rabbitmq.Ack, action)
		svc.AssertExpectations(t)
	})

	t.Run("acks an empty batch without importing", func(t *testing.T) {
		svc := &mockUserService{}
		d := delivery(t, events.EventTypeUsersImported, events.UsersImportedPayload{FileID: "file-1"})

		action := New(logger.NewLogger(false), svc).Process(d)

		require.Equal(t, rabbitmq.Ack, action)
		svc.AssertNotCalled(t, "Import", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("skips users that cannot be imported and keeps going", func(t *testing.T) {
		skippable := map[string]error{
			"taken":    users.ErrUsernameTaken,
			"emailed":  users.ErrEmailTaken,
			"reserved": users.ErrReservedUsername,
		}
		for name, importErr := range skippable {
			t.Run(name, func(t *testing.T) {
				svc := &mockUserService{}
				first, second := userPayload("first"), userPayload("second")
				expectImport(svc, first, importErr)
				expectImport(svc, second, nil)
				d := delivery(t, events.EventTypeUsersImported, events.UsersImportedPayload{FileID: "file-1", Users: []events.UserPayload{first, second}})

				action := New(logger.NewLogger(false), svc).Process(d)

				require.Equal(t, rabbitmq.Ack, action)
				svc.AssertExpectations(t)
			})
		}
	})

	t.Run("requeues and stops on an unexpected error", func(t *testing.T) {
		svc := &mockUserService{}
		first, second := userPayload("first"), userPayload("second")
		expectImport(svc, first, errors.New("db down"))
		d := delivery(t, events.EventTypeUsersImported, events.UsersImportedPayload{FileID: "file-1", Users: []events.UserPayload{first, second}})

		action := New(logger.NewLogger(false), svc).Process(d)

		require.Equal(t, rabbitmq.NackRequeue, action)
		svc.AssertExpectations(t)
		svc.AssertNotCalled(t, "Import", mock.Anything, second.Username, second.Email, second.FirstName, second.LastName)
	})

	t.Run("discards an unknown event type", func(t *testing.T) {
		svc := &mockUserService{}
		d := delivery(t, events.EventTypeFileUploaded, events.UsersImportedPayload{FileID: "file-1", Users: []events.UserPayload{userPayload("alice")}})

		action := New(logger.NewLogger(false), svc).Process(d)

		require.Equal(t, rabbitmq.NackDiscard, action)
		svc.AssertNotCalled(t, "Import", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("discards an invalid payload", func(t *testing.T) {
		svc := &mockUserService{}
		d := rabbitmq.Delivery{Delivery: amqp.Delivery{Type: events.EventTypeUsersImported, Body: []byte("not json")}}

		action := New(logger.NewLogger(false), svc).Process(d)

		require.Equal(t, rabbitmq.NackDiscard, action)
		svc.AssertNotCalled(t, "Import", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}
