package files

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockQuerier struct {
	mock.Mock
}

func (m *mockQuerier) InsertFile(ctx context.Context, arg sqlc.InsertFileParams) (sqlc.File, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlc.File), args.Error(1)
}

var (
	fileID    = uuid.MustParse("0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11")
	createdAt = time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	updatedAt = time.Date(2023, 1, 2, 12, 0, 0, 0, time.UTC)
	checksum  = []byte("0123456789abcdef0123456789abcdef")
)

func domainFile() storage.File {
	return storage.File{
		ID:             fileID.String(),
		UserID:         7,
		Filename:       "users.csv",
		ContentType:    "text/csv",
		Path:           "7/" + fileID.String(),
		RootDir:        "test-bucket",
		Size:           1024,
		ChecksumSHA256: checksum,
	}
}

func insertParams() sqlc.InsertFileParams {
	return sqlc.InsertFileParams{
		ID:             pgtype.UUID{Bytes: fileID, Valid: true},
		UserID:         7,
		Filename:       "users.csv",
		ContentType:    "text/csv",
		Path:           "7/" + fileID.String(),
		RootDir:        "test-bucket",
		Size:           1024,
		ChecksumSha256: checksum,
	}
}

func dbFile() sqlc.File {
	return sqlc.File{
		ID:             pgtype.UUID{Bytes: fileID, Valid: true},
		UserID:         7,
		Filename:       "users.csv",
		ContentType:    "text/csv",
		Path:           "7/" + fileID.String(),
		RootDir:        "test-bucket",
		Size:           1024,
		ChecksumSha256: checksum,
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:      pgtype.Timestamptz{Time: updatedAt, Valid: true},
	}
}

func TestRepository_Insert(t *testing.T) {
	t.Run("maps the domain file to params and the row back, including DB-set timestamps", func(t *testing.T) {
		q := &mockQuerier{}
		q.On("InsertFile", mock.Anything, insertParams()).Return(dbFile(), nil)

		got, err := New(q).Insert(context.Background(), domainFile())

		require.NoError(t, err)
		want := domainFile()
		want.CreatedAt = createdAt
		want.UpdatedAt = updatedAt
		require.Equal(t, want, got)
		q.AssertExpectations(t)
	})

	t.Run("passes database errors through", func(t *testing.T) {
		q := &mockQuerier{}
		dbErr := errors.New("db down")
		q.On("InsertFile", mock.Anything, mock.Anything).Return(sqlc.File{}, dbErr)

		got, err := New(q).Insert(context.Background(), domainFile())

		require.ErrorIs(t, err, dbErr)
		require.Equal(t, storage.File{}, got)
		q.AssertExpectations(t)
	})

	t.Run("rejects a malformed id without touching the database", func(t *testing.T) {
		q := &mockQuerier{}
		file := domainFile()
		file.ID = "not-a-uuid"

		got, err := New(q).Insert(context.Background(), file)

		require.Error(t, err)
		require.Equal(t, storage.File{}, got)
		q.AssertNotCalled(t, "InsertFile", mock.Anything, mock.Anything)
	})
}
