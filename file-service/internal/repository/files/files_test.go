package files

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

func (m *mockQuerier) UpdateFileStatus(ctx context.Context, arg sqlc.UpdateFileStatusParams) (sqlc.File, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlc.File), args.Error(1)
}

func (m *mockQuerier) DeleteFile(ctx context.Context, id pgtype.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockQuerier) InsertEvent(ctx context.Context, arg sqlc.InsertEventParams) (sqlc.OutboxEvent, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlc.OutboxEvent), args.Error(1)
}

func (m *mockQuerier) GetPendingFilesOlderThan(ctx context.Context, arg sqlc.GetPendingFilesOlderThanParams) ([]sqlc.File, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).([]sqlc.File), args.Error(1)
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
		Status:         sqlc.FileStatusPending,
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
		want.Status = storage.StatusPending
		want.CreatedAt = createdAt
		want.UpdatedAt = updatedAt
		require.Equal(t, want, got)
		q.AssertExpectations(t)
	})

	t.Run("maps a unique violation on user and checksum to ErrDuplicateFile", func(t *testing.T) {
		q := &mockQuerier{}
		pgErr := &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: checksumConstraint}
		q.On("InsertFile", mock.Anything, mock.Anything).Return(sqlc.File{}, fmt.Errorf("insert: %w", pgErr))

		got, err := New(q).Insert(context.Background(), domainFile())

		require.ErrorIs(t, err, storage.ErrDuplicateFile)
		require.Equal(t, storage.File{}, got)
		q.AssertExpectations(t)
	})

	t.Run("passes through unique violations on other constraints", func(t *testing.T) {
		q := &mockQuerier{}
		pgErr := &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: "files_pkey"}
		q.On("InsertFile", mock.Anything, mock.Anything).Return(sqlc.File{}, pgErr)

		_, err := New(q).Insert(context.Background(), domainFile())

		require.ErrorIs(t, err, pgErr)
		require.NotErrorIs(t, err, storage.ErrDuplicateFile)
	})

	t.Run("passes through other pg errors on the checksum constraint name", func(t *testing.T) {
		q := &mockQuerier{}
		pgErr := &pgconn.PgError{Code: "23502", ConstraintName: checksumConstraint}
		q.On("InsertFile", mock.Anything, mock.Anything).Return(sqlc.File{}, pgErr)

		_, err := New(q).Insert(context.Background(), domainFile())

		require.ErrorIs(t, err, pgErr)
		require.NotErrorIs(t, err, storage.ErrDuplicateFile)
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

func TestRepository_UpdateStatus(t *testing.T) {
	t.Run("maps the status to the query params and the row back", func(t *testing.T) {
		q := &mockQuerier{}
		row := dbFile()
		row.Status = sqlc.FileStatusUploaded
		q.On("UpdateFileStatus", mock.Anything, sqlc.UpdateFileStatusParams{
			ID:     pgtype.UUID{Bytes: fileID, Valid: true},
			Status: sqlc.FileStatusUploaded,
		}).Return(row, nil)

		got, err := New(q).UpdateStatus(context.Background(), fileID.String(), storage.StatusUploaded)

		require.NoError(t, err)
		want := domainFile()
		want.Status = storage.StatusUploaded
		want.CreatedAt = createdAt
		want.UpdatedAt = updatedAt
		require.Equal(t, want, got)
		q.AssertExpectations(t)
	})

	t.Run("passes database errors through", func(t *testing.T) {
		q := &mockQuerier{}
		dbErr := errors.New("db down")
		q.On("UpdateFileStatus", mock.Anything, mock.Anything).Return(sqlc.File{}, dbErr)

		got, err := New(q).UpdateStatus(context.Background(), fileID.String(), storage.StatusUploaded)

		require.ErrorIs(t, err, dbErr)
		require.Equal(t, storage.File{}, got)
		q.AssertExpectations(t)
	})

	t.Run("rejects a malformed id without touching the database", func(t *testing.T) {
		q := &mockQuerier{}

		got, err := New(q).UpdateStatus(context.Background(), "not-a-uuid", storage.StatusUploaded)

		require.Error(t, err)
		require.Equal(t, storage.File{}, got)
		q.AssertNotCalled(t, "UpdateFileStatus", mock.Anything, mock.Anything)
	})
}

func TestRepository_Delete(t *testing.T) {
	t.Run("deletes by id", func(t *testing.T) {
		q := &mockQuerier{}
		q.On("DeleteFile", mock.Anything, pgtype.UUID{Bytes: fileID, Valid: true}).Return(nil)

		err := New(q).Delete(context.Background(), fileID.String())

		require.NoError(t, err)
		q.AssertExpectations(t)
	})

	t.Run("passes database errors through", func(t *testing.T) {
		q := &mockQuerier{}
		dbErr := errors.New("db down")
		q.On("DeleteFile", mock.Anything, mock.Anything).Return(dbErr)

		err := New(q).Delete(context.Background(), fileID.String())

		require.ErrorIs(t, err, dbErr)
		q.AssertExpectations(t)
	})

	t.Run("rejects a malformed id without touching the database", func(t *testing.T) {
		q := &mockQuerier{}

		err := New(q).Delete(context.Background(), "not-a-uuid")

		require.Error(t, err)
		q.AssertNotCalled(t, "DeleteFile", mock.Anything, mock.Anything)
	})
}

func TestRepository_GetPendingFilesOlderThan(t *testing.T) {
	t.Run("maps the cursor and cutoff to the query params and the rows back", func(t *testing.T) {
		after := createdAt.Add(-time.Hour)
		before := createdAt.Add(time.Hour)
		q := &mockQuerier{}
		q.On("GetPendingFilesOlderThan", mock.Anything, sqlc.GetPendingFilesOlderThanParams{
			CreatedAt:   pgtype.Timestamptz{Time: after, Valid: true},
			CreatedAt_2: pgtype.Timestamptz{Time: before, Valid: true},
		}).Return([]sqlc.File{dbFile(), dbFile()}, nil)

		got, err := New(q).GetPendingFilesOlderThan(context.Background(), after, before)

		require.NoError(t, err)
		want := domainFile()
		want.Status = storage.StatusPending
		want.CreatedAt = createdAt
		want.UpdatedAt = updatedAt
		require.Equal(t, []storage.File{want, want}, got)
		q.AssertExpectations(t)
	})

	t.Run("returns no files when nothing matches", func(t *testing.T) {
		q := &mockQuerier{}
		q.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything).Return([]sqlc.File{}, nil)

		got, err := New(q).GetPendingFilesOlderThan(context.Background(), time.Time{}, createdAt)

		require.NoError(t, err)
		require.Empty(t, got)
		q.AssertExpectations(t)
	})

	t.Run("passes database errors through", func(t *testing.T) {
		q := &mockQuerier{}
		dbErr := errors.New("db down")
		q.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything).Return([]sqlc.File{}, dbErr)

		got, err := New(q).GetPendingFilesOlderThan(context.Background(), time.Time{}, createdAt)

		require.ErrorIs(t, err, dbErr)
		require.Nil(t, got)
		q.AssertExpectations(t)
	})
}
