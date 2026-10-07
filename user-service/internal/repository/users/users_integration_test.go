//go:build integration

package users

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	sqlc "github.com/aleksandarv/file-uploader/user-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const migrationsDir = "../../../db/migrations"

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("users"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("failed to start postgres container: %v", err)
		return 1
	}
	defer func() {
		if err := ctr.Terminate(ctx); err != nil {
			log.Printf("failed to terminate postgres container: %v", err)
		}
	}()

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("failed to get connection string: %v", err)
		return 1
	}

	if err := migrate(dsn); err != nil {
		log.Printf("failed to run migrations: %v", err)
		return 1
	}

	testPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		log.Printf("failed to create pool: %v", err)
		return 1
	}
	defer testPool.Close()

	return m.Run()
}

func migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, migrationsDir)
}

func newTestRepo(t *testing.T) users.Repository {
	t.Helper()

	_, err := testPool.Exec(context.Background(), "TRUNCATE users RESTART IDENTITY")
	require.NoError(t, err)

	return New(sqlc.New(testPool))
}

func newUser(username, email string) users.User {
	return users.User{Username: username, Email: email, PasswordHash: "$2a$10$hash"}
}

func TestRepositoryDB_CreateThenGetByUsername(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, newUser("john_doe", "john.doe@example.com"))
	require.NoError(t, err)

	require.Equal(t, int64(1), created.ID)
	require.Equal(t, "john_doe", created.Username)
	require.Equal(t, "john.doe@example.com", created.Email)
	require.Equal(t, "$2a$10$hash", created.PasswordHash)
	require.WithinDuration(t, time.Now(), created.CreatedAt, time.Minute)
	require.Equal(t, created.CreatedAt, created.UpdatedAt)

	got, err := repo.GetByUsername(ctx, "john_doe")
	require.NoError(t, err)
	require.Equal(t, created, got)
}

func TestRepositoryDB_GetByUsername_NotFound(t *testing.T) {
	repo := newTestRepo(t)

	got, err := repo.GetByUsername(context.Background(), "nobody")

	require.ErrorIs(t, err, users.ErrNotFound)
	require.Zero(t, got)
}

func TestRepositoryDB_Create_AssignsIncreasingIDs(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	first, err := repo.Create(ctx, newUser("alice", "alice@example.com"))
	require.NoError(t, err)
	second, err := repo.Create(ctx, newUser("bob", "bob@example.com"))
	require.NoError(t, err)

	require.Equal(t, int64(1), first.ID)
	require.Equal(t, int64(2), second.ID)
}

func TestRepositoryDB_Create_DuplicateUsername(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.Create(ctx, newUser("john_doe", "john.doe@example.com"))
	require.NoError(t, err)

	_, err = repo.Create(ctx, newUser("john_doe", "other@example.com"))

	require.ErrorIs(t, err, users.ErrUsernameTaken)
}

func TestRepositoryDB_Create_DuplicateEmailIsCaseInsensitive(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.Create(ctx, newUser("john_doe", "John.Doe@Example.com"))
	require.NoError(t, err)

	_, err = repo.Create(ctx, newUser("jane_doe", "john.doe@example.com"))

	require.ErrorIs(t, err, users.ErrEmailTaken)
}

func TestRepositoryDB_Create_SchemaConstraints(t *testing.T) {
	tests := []struct {
		name     string
		user     users.User
		wantCode string
	}{
		{"uppercase username violates check constraint", newUser("John", "john@example.com"), "23514"},
		{"username longer than 50 characters", newUser(strings.Repeat("a", 51), "long@example.com"), "22001"},
		{"email longer than 254 characters", newUser("long_email", strings.Repeat("a", 250)+"@x.com"), "22001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newTestRepo(t)

			_, err := repo.Create(context.Background(), tt.user)

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, tt.wantCode, pgErr.Code)
		})
	}
}
