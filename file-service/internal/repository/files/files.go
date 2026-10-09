package files

import (
	"context"
	"fmt"

	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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
	if err != nil {
		return storage.File{}, err
	}
	return toFile(f), nil
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
		CreatedAt:      f.CreatedAt.Time,
		UpdatedAt:      f.UpdatedAt.Time,
	}
}
