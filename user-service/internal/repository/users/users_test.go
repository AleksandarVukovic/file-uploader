package users

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlc "github.com/aleksandarv/file-uploader/user-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockQuerier struct {
	mock.Mock
}

func (m *mockQuerier) GetByUsername(ctx context.Context, username string) (sqlc.User, error) {
	args := m.Called(ctx, username)
	return args.Get(0).(sqlc.User), args.Error(1)
}

func (m *mockQuerier) CreateUser(ctx context.Context, arg sqlc.CreateUserParams) (sqlc.User, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlc.User), args.Error(1)
}

var (
	createdAt = time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	updatedAt = time.Date(2023, 1, 2, 12, 0, 0, 0, time.UTC)
)

func dbUser() sqlc.User {
	return sqlc.User{
		ID:           5,
		Username:     "john_doe",
		Email:        "john.doe@example.com",
		PasswordHash: "hash",
		CreatedAt:    pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: updatedAt, Valid: true},
	}
}

func wantUser() users.User {
	return users.User{
		ID:           5,
		Username:     "john_doe",
		Email:        "john.doe@example.com",
		PasswordHash: "hash",
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
}

func TestRepository_GetByUsername(t *testing.T) {
	tests := []struct {
		name    string
		dbUser  sqlc.User
		dbErr   error
		want    users.User
		wantErr error
	}{
		{name: "maps the row to the domain user", dbUser: dbUser(), want: wantUser()},
		{name: "maps no rows to ErrNotFound", dbErr: pgx.ErrNoRows, wantErr: users.ErrNotFound},
		{name: "passes other errors through", dbErr: errors.New("db down"), wantErr: errors.New("db down")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &mockQuerier{}
			q.On("GetByUsername", mock.Anything, "john_doe").Return(tt.dbUser, tt.dbErr)

			got, err := New(q).GetByUsername(context.Background(), "john_doe")

			q.AssertExpectations(t)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.Equal(t, tt.wantErr.Error(), err.Error())
				require.Zero(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRepository_Create(t *testing.T) {
	newUser := users.User{Username: "john_doe", Email: "john.doe@example.com", PasswordHash: "hash"}

	tests := []struct {
		name    string
		dbErr   error
		wantErr error
	}{
		{name: "success"},
		{
			name:    "username unique violation",
			dbErr:   &pgconn.PgError{Code: "23505", ConstraintName: "users_username_key"},
			wantErr: users.ErrUsernameTaken,
		},
		{
			name:    "email unique violation",
			dbErr:   &pgconn.PgError{Code: "23505", ConstraintName: "users_email_lower_key"},
			wantErr: users.ErrEmailTaken,
		},
		{
			name:  "unknown unique violation is passed through",
			dbErr: &pgconn.PgError{Code: "23505", ConstraintName: "something_else"},
		},
		{
			name:  "other postgres error is passed through",
			dbErr: &pgconn.PgError{Code: "23514", ConstraintName: "users_username_check"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := dbUser()
			if tt.dbErr != nil {
				row = sqlc.User{}
			}
			q := &mockQuerier{}
			q.On("CreateUser", mock.Anything, sqlc.CreateUserParams{
				Username:     newUser.Username,
				Email:        newUser.Email,
				PasswordHash: newUser.PasswordHash,
			}).Return(row, tt.dbErr)

			got, err := New(q).Create(context.Background(), newUser)

			q.AssertExpectations(t)

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
				require.Zero(t, got)
			case tt.dbErr != nil:
				require.ErrorIs(t, err, tt.dbErr)
				require.Zero(t, got)
			default:
				require.NoError(t, err)
				require.Equal(t, wantUser(), got)
			}
		})
	}
}
