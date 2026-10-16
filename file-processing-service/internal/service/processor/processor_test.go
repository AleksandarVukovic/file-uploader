package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/messaging"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wagslane/go-rabbitmq"
)

const (
	testBucket = "test-bucket"
	testKey    = "42/0b9f6c1e-5c0e-4f55-9d0e-3f1d5f0f9a11"
	testFileID = "0b9f6c1e-5c0e-4f55-9d0e-3f1d5f0f9a11"
)

const validCSV = "id,username,name,email\n" +
	"1,alice,Alice Johnson,alice@example.com\n" +
	"2,bob,Bob Smith,Bob@Example.com\n"

const mixedCSV = "id,username,name,email\n" +
	"1,alice,Alice Johnson,alice@example.com\n" +
	"x,bad_id,Bad Id,bad@example.com\n" +
	"3,Bad_Username,Bad Username,badu@example.com\n" +
	"4,onename,Alice,onename@example.com\n" +
	"5,three,Three Part Name,three@example.com\n" +
	"6,bad_email,Bad Email,not-an-email\n" +
	"7,short_row,Short Row\n" +
	"8,da\"ve,Dave Lee,dave@example.com\n" +
	"9,eve,Eve Adams,eve@example.com\n"

type mockObjectStore struct {
	mock.Mock
}

func (m *mockObjectStore) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	args := m.Called(ctx, bucket, key)
	var body io.ReadCloser
	if b := args.Get(0); b != nil {
		body = b.(io.ReadCloser)
	}
	return body, args.Error(1)
}

type mockPublisher struct {
	mock.Mock
}

func (m *mockPublisher) Publish(ctx context.Context, msg messaging.Message) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func newDelivery(t *testing.T, eventType string, payload events.FileUploadedPayload) rabbitmq.Delivery {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return rabbitmq.Delivery{Delivery: amqp.Delivery{Type: eventType, Body: body, MessageId: "msg-1"}}
}

func newPayload() events.FileUploadedPayload {
	return events.FileUploadedPayload{
		FileID:  testFileID,
		UserID:  42,
		RootDir: testBucket,
		Path:    testKey,
	}
}

func objectBody(content string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(content))
}

func batchMessage(batch, users int) any {
	return mock.MatchedBy(func(msg messaging.Message) bool {
		var payload events.UsersImportedPayload
		if err := json.Unmarshal(msg.Body, &payload); err != nil {
			return false
		}
		return msg.ID == fmt.Sprintf("%s-%d", testFileID, batch) &&
			msg.Type == events.EventTypeUsersImported &&
			msg.RoutingKey == events.RoutingKeyUsersImported &&
			payload.FileID == testFileID &&
			payload.Batch == batch &&
			len(payload.Users) == users
	})
}

func TestProcess(t *testing.T) {
	tests := []struct {
		name       string
		delivery   func(t *testing.T) rabbitmq.Delivery
		setup      func(store *mockObjectStore, pub *mockPublisher)
		want       rabbitmq.Action
		storeHit   bool
		publishHit bool
	}{
		{
			name: "acks and publishes the users of a valid file",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(validCSV), error(nil))
				pub.On("Publish", mock.Anything, batchMessage(0, 2)).Return(error(nil))
			},
			want:       rabbitmq.Ack,
			storeHit:   true,
			publishHit: true,
		},
		{
			name: "reads from the bucket named in the event",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				payload := newPayload()
				payload.RootDir = "another-bucket"
				return newDelivery(t, events.EventTypeFileUploaded, payload)
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, "another-bucket", testKey).Return(objectBody(validCSV), error(nil))
				pub.On("Publish", mock.Anything, batchMessage(0, 2)).Return(error(nil))
			},
			want:       rabbitmq.Ack,
			storeHit:   true,
			publishHit: true,
		},
		{
			name: "acks a file with only a header without publishing",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody("id,username,name,email\n"), error(nil))
			},
			want:     rabbitmq.Ack,
			storeHit: true,
		},
		{
			name: "skips invalid and malformed rows and publishes the valid ones",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(mixedCSV), error(nil))
				pub.On("Publish", mock.Anything, batchMessage(0, 2)).Return(error(nil))
			},
			want:       rabbitmq.Ack,
			storeHit:   true,
			publishHit: true,
		},
		{
			name: "acks a file where every row is invalid without publishing",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody("id,username,name,email\nx,a,A B,c\n"), error(nil))
			},
			want:     rabbitmq.Ack,
			storeHit: true,
		},
		{
			name: "discards an empty file",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(""), error(nil))
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "discards a file with an unexpected header",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody("id,name\n1,alice\n"), error(nil))
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "discards when the object does not exist",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(nil, ErrObjectNotFound)
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "discards when the bucket is not allowed",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(nil, ErrBucketNotAllowed)
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "requeues when the object store fails",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(nil, errors.New("s3 unavailable"))
			},
			want:     rabbitmq.NackRequeue,
			storeHit: true,
		},
		{
			name: "requeues when reading the object fails",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(io.NopCloser(failingReader{}), error(nil))
			},
			want:     rabbitmq.NackRequeue,
			storeHit: true,
		},
		{
			name: "requeues when publishing fails",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload())
			},
			setup: func(store *mockObjectStore, pub *mockPublisher) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(validCSV), error(nil))
				pub.On("Publish", mock.Anything, batchMessage(0, 2)).Return(errors.New("broker unavailable"))
			},
			want:       rabbitmq.NackRequeue,
			storeHit:   true,
			publishHit: true,
		},
		{
			name: "discards an unknown event type",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, "FileDeleted", newPayload())
			},
			setup: func(*mockObjectStore, *mockPublisher) {},
			want:  rabbitmq.NackDiscard,
		},
		{
			name: "discards an invalid payload",
			delivery: func(*testing.T) rabbitmq.Delivery {
				return rabbitmq.Delivery{Delivery: amqp.Delivery{Type: events.EventTypeFileUploaded, Body: []byte("not json")}}
			},
			setup: func(*mockObjectStore, *mockPublisher) {},
			want:  rabbitmq.NackDiscard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockObjectStore{}
			pub := &mockPublisher{}
			tt.setup(store, pub)
			p := New(logger.NewLogger(false), store, pub)

			got := p.Process(tt.delivery(t))

			require.Equal(t, tt.want, got)
			if tt.storeHit {
				store.AssertExpectations(t)
			} else {
				store.AssertNotCalled(t, "Get", mock.Anything, mock.Anything, mock.Anything)
			}
			if tt.publishHit {
				pub.AssertExpectations(t)
			} else {
				pub.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
			}
		})
	}
}

func TestProcessPublishesParsedUsers(t *testing.T) {
	store := &mockObjectStore{}
	store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(validCSV), error(nil))

	var published messaging.Message
	pub := &mockPublisher{}
	pub.On("Publish", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { published = args.Get(1).(messaging.Message) }).
		Return(error(nil))

	p := New(logger.NewLogger(false), store, pub)
	got := p.Process(newDelivery(t, events.EventTypeFileUploaded, newPayload()))

	require.Equal(t, rabbitmq.Ack, got)
	require.Equal(t, testFileID+"-0", published.ID)
	require.Equal(t, events.EventTypeUsersImported, published.Type)
	require.Equal(t, events.RoutingKeyUsersImported, published.RoutingKey)

	var payload events.UsersImportedPayload
	require.NoError(t, json.Unmarshal(published.Body, &payload))
	require.Equal(t, events.UsersImportedPayload{
		FileID: testFileID,
		Batch:  0,
		Users: []events.UserPayload{
			{ID: 1, Username: "alice", FirstName: "Alice", LastName: "Johnson", Email: "alice@example.com"},
			{ID: 2, Username: "bob", FirstName: "Bob", LastName: "Smith", Email: "bob@example.com"},
		},
	}, payload)
	store.AssertExpectations(t)
	pub.AssertExpectations(t)
}

func TestProcessPublishesUsersInBatches(t *testing.T) {
	var content strings.Builder
	content.WriteString("id,username,name,email\n")
	for i := 1; i <= 2*publishBatchSize+20; i++ {
		fmt.Fprintf(&content, "%d,user%d,User Name,user%d@example.com\n", i, i, i)
	}

	store := &mockObjectStore{}
	store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(content.String()), error(nil))
	pub := &mockPublisher{}
	pub.On("Publish", mock.Anything, batchMessage(0, publishBatchSize)).Return(error(nil)).Once()
	pub.On("Publish", mock.Anything, batchMessage(1, publishBatchSize)).Return(error(nil)).Once()
	pub.On("Publish", mock.Anything, batchMessage(2, 20)).Return(error(nil)).Once()

	p := New(logger.NewLogger(false), store, pub)
	got := p.Process(newDelivery(t, events.EventTypeFileUploaded, newPayload()))

	require.Equal(t, rabbitmq.Ack, got)
	store.AssertExpectations(t)
	pub.AssertExpectations(t)
	pub.AssertNumberOfCalls(t, "Publish", 3)
}

func TestProcessStopsPublishingAfterFirstFailure(t *testing.T) {
	var content strings.Builder
	content.WriteString("id,username,name,email\n")
	for i := 1; i <= 2*publishBatchSize; i++ {
		fmt.Fprintf(&content, "%d,user%d,User Name,user%d@example.com\n", i, i, i)
	}

	store := &mockObjectStore{}
	store.On("Get", mock.Anything, testBucket, testKey).Return(objectBody(content.String()), error(nil))
	pub := &mockPublisher{}
	pub.On("Publish", mock.Anything, batchMessage(0, publishBatchSize)).Return(errors.New("broker unavailable")).Once()

	p := New(logger.NewLogger(false), store, pub)
	got := p.Process(newDelivery(t, events.EventTypeFileUploaded, newPayload()))

	require.Equal(t, rabbitmq.NackRequeue, got)
	pub.AssertExpectations(t)
	pub.AssertNumberOfCalls(t, "Publish", 1)
}
