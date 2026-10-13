package files

import (
	"context"
	"errors"
	"fmt"
	"time"

	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	pgUniqueViolation  = "23505"
	checksumConstraint = "files_user_id_checksum_sha256_key"
)

type repository struct {
	queries sqlc.Querier
}

func New(queries sqlc.Querier) storage.Repository {
	return &repository{queries: queries}
}

func (r *repository) Insert(ctx context.Context, file storage.File) (storage.File, error) {
	id, err := uuid.Parse(file.ID)
	if err != nil {
		return storage.File{}, fmt.Errorf("invalid file id %q: %w", file.ID, err)
	}

	f, err := r.queries.InsertFile(ctx, sqlc.InsertFileParams{
		ID:             pgtype.UUID{Bytes: id, Valid: true},
		UserID:         file.UserID,
		Filename:       file.Filename,
		ContentType:    file.ContentType,
		Path:           file.Path,
		RootDir:        file.RootDir,
		Size:           file.Size,
		ChecksumSha256: file.ChecksumSHA256,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == checksumConstraint {
		return storage.File{}, storage.ErrDuplicateFile
	}
	if err != nil {
		return storage.File{}, err
	}
	return toFile(f), nil
}

func (r *repository) UpdateStatus(ctx context.Context, fileID string, status storage.Status) (storage.File, error) {
	id, err := uuid.Parse(fileID)
	if err != nil {
		return storage.File{}, fmt.Errorf("invalid file id %q: %w", fileID, err)
	}

	f, err := r.queries.UpdateFileStatus(ctx, sqlc.UpdateFileStatusParams{
		ID:     pgtype.UUID{Bytes: id, Valid: true},
		Status: sqlc.FileStatus(status),
	})
	if err != nil {
		return storage.File{}, err
	}
	return toFile(f), nil
}

func (r *repository) Delete(ctx context.Context, fileID string) error {
	id, err := uuid.Parse(fileID)
	if err != nil {
		return fmt.Errorf("invalid file id %q: %w", fileID, err)
	}

	return r.queries.DeleteFile(ctx, pgtype.UUID{Bytes: id, Valid: true})
}

func (r *repository) GetPendingFilesOlderThan(ctx context.Context, after, before time.Time) ([]storage.File, error) {
	files, err := r.queries.GetPendingFilesOlderThan(ctx, sqlc.GetPendingFilesOlderThanParams{
		CreatedAt:   pgtype.Timestamptz{Time: after, Valid: true},
		CreatedAt_2: pgtype.Timestamptz{Time: before, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	var result []storage.File
	for _, f := range files {
		result = append(result, toFile(f))
	}
	return result, nil
}

func toFile(f sqlc.File) storage.File {
	return storage.File{
		ID:             uuid.UUID(f.ID.Bytes).String(),
		UserID:         f.UserID,
		Filename:       f.Filename,
		ContentType:    f.ContentType,
		Path:           f.Path,
		RootDir:        f.RootDir,
		Size:           f.Size,
		ChecksumSHA256: f.ChecksumSha256,
		Status:         storage.Status(f.Status),
		CreatedAt:      f.CreatedAt.Time,
		UpdatedAt:      f.UpdatedAt.Time,
	}
}
