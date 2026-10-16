//go:build integration

package outbox

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/aleksandarv/file-uploader/common/db"
	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
	filesrepo "github.com/aleksandarv/file-uploader/file-service/internal/repository/files"
	sqlc "github.com/aleksandarv/file-uploader/file-service/internal/repository/gen"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/google/uuid"
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
		postgres.WithDatabase("files"),
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
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(conn, migrationsDir)
}

type txFixture struct {
	files      storage.Repository
	outbox     outbox.Repository
	transactor storage.Transactor
}

func newFixture(t *testing.T) txFixture {
	t.Helper()

	_, err := testPool.Exec(context.Background(), "TRUNCATE files, outbox_events")
	require.NoError(t, err)

	queries := sqlc.New(db.NewExecutor(testPool))
	return txFixture{
		files:      filesrepo.New(queries),
		outbox:     New(queries),
		transactor: db.NewTransactor(testPool),
	}
}

func (f txFixture) insertPendingFile(t *testing.T) storage.File {
	t.Helper()

	id := uuid.NewString()
	digest := sha256.Sum256([]byte(id))
	file, err := f.files.Insert(context.Background(), storage.File{
		ID:             id,
		UserID:         7,
		Filename:       "users.csv",
		ContentType:    "text/csv",
		Path:           "7/" + id,
		RootDir:        "test-bucket",
		Size:           1024,
		ChecksumSHA256: digest[:],
		Status:         storage.StatusPending,
	})
	require.NoError(t, err)
	return file
}

func newEvent(aggregateID string) outbox.Event {
	return outbox.Event{
		ID:            uuid.NewString(),
		AggregateType: events.AggregateTypeFile,
		AggregateID:   aggregateID,
		EventType:     events.EventTypeFileUploaded,
		Payload:       []byte(`{}`),
	}
}

func (f txFixture) insertEvents(t *testing.T, n int) []outbox.Event {
	t.Helper()

	inserted := make([]outbox.Event, 0, n)
	for i := 0; i < n; i++ {
		event := newEvent(uuid.NewString())
		require.NoError(t, f.outbox.Insert(context.Background(), event))
		inserted = append(inserted, event)
	}
	return inserted
}

func countEvents(t *testing.T) int {
	t.Helper()

	var n int
	require.NoError(t, testPool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events").Scan(&n))
	return n
}

func countPublishedEvents(t *testing.T) int {
	t.Helper()

	var n int
	require.NoError(t, testPool.QueryRow(context.Background(), "SELECT count(*) FROM outbox_events WHERE published_at IS NOT NULL").Scan(&n))
	return n
}

func fileStatus(t *testing.T, id string) string {
	t.Helper()

	var status string
	require.NoError(t, testPool.QueryRow(context.Background(), "SELECT status::text FROM files WHERE id = $1", id).Scan(&status))
	return status
}

func TestTransactor_CommitsStatusUpdateAndOutboxEventTogether(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	file := f.insertPendingFile(t)

	err := f.transactor.WithinTx(ctx, func(txCtx context.Context) error {
		if _, err := f.files.UpdateStatus(txCtx, file.ID, storage.StatusUploaded); err != nil {
			return err
		}
		return f.outbox.Insert(txCtx, newEvent(file.ID))
	})

	require.NoError(t, err)
	require.Equal(t, string(storage.StatusUploaded), fileStatus(t, file.ID))
	require.Equal(t, 1, countEvents(t))
}

func TestTransactor_RollsBackStatusUpdateWhenOutboxInsertFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	file := f.insertPendingFile(t)
	invalid := newEvent(file.ID)
	invalid.Payload = []byte(`not json`)

	err := f.transactor.WithinTx(ctx, func(txCtx context.Context) error {
		if _, err := f.files.UpdateStatus(txCtx, file.ID, storage.StatusUploaded); err != nil {
			return err
		}
		return f.outbox.Insert(txCtx, invalid)
	})

	require.Error(t, err)
	require.Equal(t, string(storage.StatusPending), fileStatus(t, file.ID), "the status update must be rolled back")
	require.Equal(t, 0, countEvents(t))
}

func TestTransactor_RollsBackWhenCallbackReturnsError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	file := f.insertPendingFile(t)
	boom := errors.New("boom")

	err := f.transactor.WithinTx(ctx, func(txCtx context.Context) error {
		if _, err := f.files.UpdateStatus(txCtx, file.ID, storage.StatusUploaded); err != nil {
			return err
		}
		if err := f.outbox.Insert(txCtx, newEvent(file.ID)); err != nil {
			return err
		}
		return boom
	})

	require.ErrorIs(t, err, boom)
	require.Equal(t, string(storage.StatusPending), fileStatus(t, file.ID))
	require.Equal(t, 0, countEvents(t), "the outbox event must be rolled back too")
}

func TestTransactor_RollsBackAndRepanicsOnPanic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	file := f.insertPendingFile(t)

	require.PanicsWithValue(t, "kaboom", func() {
		_ = f.transactor.WithinTx(ctx, func(txCtx context.Context) error {
			if _, err := f.files.UpdateStatus(txCtx, file.ID, storage.StatusUploaded); err != nil {
				return err
			}
			panic("kaboom")
		})
	})

	require.Equal(t, string(storage.StatusPending), fileStatus(t, file.ID))
}

func TestTransactor_NestedCallJoinsOuterTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	file := f.insertPendingFile(t)
	boom := errors.New("boom")

	err := f.transactor.WithinTx(ctx, func(txCtx context.Context) error {
		inner := f.transactor.WithinTx(txCtx, func(innerTxCtx context.Context) error {
			return f.outbox.Insert(innerTxCtx, newEvent(file.ID))
		})
		require.NoError(t, inner)
		return boom
	})

	require.ErrorIs(t, err, boom)
	require.Equal(t, 0, countEvents(t), "the inner write must roll back with the outer transaction")
}

func TestExecutor_UsesPoolOutsideTransaction(t *testing.T) {
	f := newFixture(t)
	file := f.insertPendingFile(t)

	require.NoError(t, f.outbox.Insert(context.Background(), newEvent(file.ID)))

	require.Equal(t, 1, countEvents(t))
}

func TestRepository_GetUnpublished_ReturnsOldestFirstUpToLimit(t *testing.T) {
	f := newFixture(t)
	inserted := f.insertEvents(t, 5)

	got, err := f.outbox.GetUnpublished(context.Background(), events.AggregateTypeFile, 3)

	require.NoError(t, err)
	require.Equal(t, inserted[:3], got)
}

func TestRepository_GetUnpublished_SkipsPublishedEvents(t *testing.T) {
	f := newFixture(t)
	inserted := f.insertEvents(t, 3)
	require.NoError(t, f.outbox.MarkPublished(context.Background(), inserted[0].ID))

	got, err := f.outbox.GetUnpublished(context.Background(), events.AggregateTypeFile, 10)

	require.NoError(t, err)
	require.Equal(t, inserted[1:], got)
}

func TestRepository_GetUnpublished_OnlyReturnsRequestedAggregateType(t *testing.T) {
	f := newFixture(t)
	fileEvents := f.insertEvents(t, 2)
	other := newEvent(uuid.NewString())
	other.AggregateType = "other"
	require.NoError(t, f.outbox.Insert(context.Background(), other))

	got, err := f.outbox.GetUnpublished(context.Background(), events.AggregateTypeFile, 10)

	require.NoError(t, err)
	require.Equal(t, fileEvents, got)
}

func TestRepository_GetUnpublished_SkipsRowsLockedByAnotherTransaction(t *testing.T) {
	f := newFixture(t)
	inserted := f.insertEvents(t, 2)

	err := f.transactor.WithinTx(context.Background(), func(txCtx context.Context) error {
		locked, err := f.outbox.GetUnpublished(txCtx, events.AggregateTypeFile, 10)
		require.NoError(t, err)
		require.Equal(t, inserted, locked)

		concurrent, err := f.outbox.GetUnpublished(context.Background(), events.AggregateTypeFile, 10)
		require.NoError(t, err)
		require.Empty(t, concurrent, "rows locked by an open transaction must be skipped")
		return nil
	})

	require.NoError(t, err)
}

func TestRepository_MarkPublished_SetsPublishedAt(t *testing.T) {
	f := newFixture(t)
	inserted := f.insertEvents(t, 2)

	require.NoError(t, f.outbox.MarkPublished(context.Background(), inserted[0].ID))

	require.Equal(t, 1, countPublishedEvents(t))
}

func TestRepository_MarkPublished_IsIdempotent(t *testing.T) {
	f := newFixture(t)
	inserted := f.insertEvents(t, 1)

	require.NoError(t, f.outbox.MarkPublished(context.Background(), inserted[0].ID))
	require.NoError(t, f.outbox.MarkPublished(context.Background(), inserted[0].ID))

	require.Equal(t, 1, countPublishedEvents(t))
}
