package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wagslane/go-rabbitmq"
)

const (
	testBucket = "test-bucket"
	testKey    = "42/0b9f6c1e-5c0e-4f55-9d0e-3f1d5f0f9a11"
)

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

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func checksumOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func newDelivery(t *testing.T, eventType string, payload events.FileUploadedPayload) rabbitmq.Delivery {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return rabbitmq.Delivery{Delivery: amqp.Delivery{Type: eventType, Body: body, MessageId: "msg-1"}}
}

func newPayload(content string) events.FileUploadedPayload {
	return events.FileUploadedPayload{
		FileID:   "0b9f6c1e-5c0e-4f55-9d0e-3f1d5f0f9a11",
		UserID:   42,
		RootDir:  testBucket,
		Path:     testKey,
		Size:     int64(len(content)),
		Checksum: checksumOf(content),
	}
}

func TestProcess(t *testing.T) {
	const content = "id,name\n1,alice\n"

	tests := []struct {
		name     string
		delivery func(t *testing.T) rabbitmq.Delivery
		setup    func(store *mockObjectStore)
		want     rabbitmq.Action
		storeHit bool
	}{
		{
			name: "acks when the object matches the checksum",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload(content))
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(io.NopCloser(strings.NewReader(content)), error(nil))
			},
			want:     rabbitmq.Ack,
			storeHit: true,
		},
		{
			name: "reads from the bucket named in the event",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				payload := newPayload(content)
				payload.RootDir = "another-bucket"
				return newDelivery(t, events.EventTypeFileUploaded, payload)
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, "another-bucket", testKey).Return(io.NopCloser(strings.NewReader(content)), error(nil))
			},
			want:     rabbitmq.Ack,
			storeHit: true,
		},
		{
			name: "discards when the checksum does not match",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload(content))
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(io.NopCloser(strings.NewReader("tampered")), error(nil))
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "discards when the object does not exist",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload(content))
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(nil, ErrObjectNotFound)
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "discards when the bucket is not allowed",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload(content))
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(nil, ErrBucketNotAllowed)
			},
			want:     rabbitmq.NackDiscard,
			storeHit: true,
		},
		{
			name: "requeues when the object store fails",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload(content))
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(nil, errors.New("s3 unavailable"))
			},
			want:     rabbitmq.NackRequeue,
			storeHit: true,
		},
		{
			name: "requeues when reading the object fails",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, events.EventTypeFileUploaded, newPayload(content))
			},
			setup: func(store *mockObjectStore) {
				store.On("Get", mock.Anything, testBucket, testKey).Return(io.NopCloser(failingReader{}), error(nil))
			},
			want:     rabbitmq.NackRequeue,
			storeHit: true,
		},
		{
			name: "discards an unknown event type",
			delivery: func(t *testing.T) rabbitmq.Delivery {
				return newDelivery(t, "FileDeleted", newPayload(content))
			},
			setup: func(*mockObjectStore) {},
			want:  rabbitmq.NackDiscard,
		},
		{
			name: "discards an invalid payload",
			delivery: func(*testing.T) rabbitmq.Delivery {
				return rabbitmq.Delivery{Delivery: amqp.Delivery{Type: events.EventTypeFileUploaded, Body: []byte("not json")}}
			},
			setup: func(*mockObjectStore) {},
			want:  rabbitmq.NackDiscard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockObjectStore{}
			tt.setup(store)
			p := New(logger.NewLogger(false), store)

			got := p.Process(tt.delivery(t))

			require.Equal(t, tt.want, got)
			if tt.storeHit {
				store.AssertExpectations(t)
			} else {
				store.AssertNotCalled(t, "Get", mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}
