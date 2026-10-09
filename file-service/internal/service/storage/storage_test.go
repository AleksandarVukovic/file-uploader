package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testRootDir = "test-bucket"

type mockObjectStore struct {
	mock.Mock
}

func (m *mockObjectStore) Put(ctx context.Context, in PutInput) error {
	args := m.Called(ctx, in)
	return args.Error(0)
}

type mockRepository struct {
	mock.Mock
}

func (m *mockRepository) Insert(ctx context.Context, file File) (File, error) {
	args := m.Called(ctx, file)
	return args.Get(0).(File), args.Error(1)
}

func newService(store ObjectStore, repo Repository) Service {
	return New(store, repo, testRootDir)
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
	createdAt := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	var gotKey string
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.MatchedBy(func(p PutInput) bool {
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

	var gotFile File
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { gotFile = args.Get(1).(File) }).
		Return(File{CreatedAt: createdAt}, nil)

	res, err := newService(store, repo).Store(context.Background(), in)

	require.NoError(t, err)
	_, err = uuid.Parse(res.UUID)
	require.NoError(t, err)
	require.Equal(t, "7/"+res.UUID, gotKey)
	require.Equal(t, in.Filename, res.Filename)
	require.Equal(t, in.ContentType, res.ContentType)
	require.Equal(t, in.Size, res.Size)
	require.Equal(t, createdAt, res.CreatedAt)
	require.Equal(t, File{
		ID:             res.UUID,
		UserID:         7,
		Filename:       in.Filename,
		ContentType:    in.ContentType,
		Path:           gotKey,
		RootDir:        testRootDir,
		Size:           in.Size,
		ChecksumSHA256: sum[:],
	}, gotFile)
	store.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestService_Store_GeneratesUniqueUUIDs(t *testing.T) {
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	svc := newService(store, repo)
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
	store := new(mockObjectStore)
	repo := new(mockRepository)

	_, err := newService(store, repo).Store(context.Background(), StoreInput{Checksum: "not-hex", Body: strings.NewReader("x")})

	require.ErrorIs(t, err, ErrInvalidChecksum)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Insert", mock.Anything, mock.Anything)
}

func TestService_Store_ReturnsObjectStoreError(t *testing.T) {
	boom := errors.New("storage unreachable")
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(boom)
	repo := new(mockRepository)

	res, err := newService(store, repo).Store(context.Background(), StoreInput{Checksum: strings.Repeat("a", 64), Body: strings.NewReader("x")})

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	store.AssertExpectations(t)
	repo.AssertNotCalled(t, "Insert", mock.Anything, mock.Anything)
}

func TestService_Store_ReturnsRepositoryError(t *testing.T) {
	boom := errors.New("db down")
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, boom)

	res, err := newService(store, repo).Store(context.Background(), StoreInput{Checksum: strings.Repeat("a", 64), Body: strings.NewReader("x")})

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	store.AssertExpectations(t)
	repo.AssertExpectations(t)
}
