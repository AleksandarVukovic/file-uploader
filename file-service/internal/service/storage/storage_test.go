package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockObjectStore struct {
	mock.Mock
}

func (m *mockObjectStore) Put(ctx context.Context, in PutInput) error {
	args := m.Called(ctx, in)
	return args.Error(0)
}

func TestService_Store_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	sum := sha256.Sum256([]byte(body))
	in := StoreInput{
		UserID:      7,
		Filename:    "users.csv",
		ContentType: "text/csv",
		Size:        int64(len(body)),
		Checksum:    hex.EncodeToString(sum[:]),
		Body:        strings.NewReader(body),
	}

	var gotKey string
	m := new(mockObjectStore)
	m.On("Put", mock.Anything, mock.MatchedBy(func(p PutInput) bool {
		return p.Size == in.Size &&
			p.ContentType == in.ContentType &&
			hex.EncodeToString(p.ChecksumSHA256) == in.Checksum
	})).
		Run(func(args mock.Arguments) {
			put := args.Get(1).(PutInput)
			gotKey = put.Key
			b, err := io.ReadAll(put.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(nil)

	res, err := New(m).Store(context.Background(), in)

	require.NoError(t, err)
	_, err = uuid.Parse(res.UUID)
	require.NoError(t, err)
	require.Equal(t, "7/"+res.UUID, gotKey)
	require.Equal(t, in.Filename, res.Filename)
	require.Equal(t, in.ContentType, res.ContentType)
	require.Equal(t, in.Size, res.Size)
	m.AssertExpectations(t)
}

func TestService_Store_GeneratesUniqueUUIDs(t *testing.T) {
	m := new(mockObjectStore)
	m.On("Put", mock.Anything, mock.Anything).Return(nil)
	svc := New(m)
	in := func() StoreInput {
		return StoreInput{UserID: 7, Checksum: strings.Repeat("a", 64), Body: strings.NewReader("x")}
	}

	first, err := svc.Store(context.Background(), in())
	require.NoError(t, err)
	second, err := svc.Store(context.Background(), in())
	require.NoError(t, err)

	require.NotEqual(t, first.UUID, second.UUID)
}

func TestService_Store_RejectsMalformedChecksum(t *testing.T) {
	m := new(mockObjectStore)

	_, err := New(m).Store(context.Background(), StoreInput{Checksum: "not-hex", Body: strings.NewReader("x")})

	require.ErrorIs(t, err, ErrInvalidChecksum)
	m.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
}

func TestService_Store_ReturnsObjectStoreError(t *testing.T) {
	boom := errors.New("storage unreachable")
	m := new(mockObjectStore)
	m.On("Put", mock.Anything, mock.Anything).Return(boom)

	res, err := New(m).Store(context.Background(), StoreInput{Checksum: strings.Repeat("a", 64), Body: strings.NewReader("x")})

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	m.AssertExpectations(t)
}
