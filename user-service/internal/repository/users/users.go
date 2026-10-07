package users

import (
	"context"
	"errors"

	sqlc "github.com/aleksandarv/file-uploader/user-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	pgUniqueViolation  = "23505"
	usernameConstraint = "users_username_key"
	emailConstraint    = "users_email_lower_key"
)

type repository struct {
	queries sqlc.Querier
}

func New(queries sqlc.Querier) users.Repository {
	return &repository{queries: queries}
}

func (r *repository) GetByUsername(ctx context.Context, username string) (users.User, error) {
	u, err := r.queries.GetByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return users.User{}, users.ErrNotFound
	}
	if err != nil {
		return users.User{}, err
	}
	return toUser(u), nil
}

func (r *repository) Create(ctx context.Context, user users.User) (users.User, error) {
	u, err := r.queries.CreateUser(ctx, sqlc.CreateUserParams{
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		switch pgErr.ConstraintName {
		case usernameConstraint:
			return users.User{}, users.ErrUsernameTaken
		case emailConstraint:
			return users.User{}, users.ErrEmailTaken
		}
	}
	if err != nil {
		return users.User{}, err
	}
	return toUser(u), nil
}

func toUser(u sqlc.User) users.User {
	return users.User{
		ID:           u.ID,
		Username:     u.Username,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		CreatedAt:    u.CreatedAt.Time,
		UpdatedAt:    u.UpdatedAt.Time,
	}
}
