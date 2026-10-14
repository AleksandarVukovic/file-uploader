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

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testRootDir        = "test-bucket"
	testPendingFileTTL = 30 * time.Minute
)

type mockObjectStore struct {
	mock.Mock
}

func (m *mockObjectStore) Put(ctx context.Context, in PutInput) error {
	args := m.Called(ctx, in)
	return args.Error(0)
}

func (m *mockObjectStore) Exists(ctx context.Context, key string) (bool, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Error(1)
}

type mockRepository struct {
	mock.Mock
}

func (m *mockRepository) Insert(ctx context.Context, file File) (File, error) {
	args := m.Called(ctx, file)
	return args.Get(0).(File), args.Error(1)
}

func (m *mockRepository) UpdateStatus(ctx context.Context, id string, status Status) (File, error) {
	args := m.Called(ctx, id, status)
	return args.Get(0).(File), args.Error(1)
}

func (m *mockRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockRepository) GetPendingFilesOlderThan(ctx context.Context, after, before time.Time) ([]File, error) {
	args := m.Called(ctx, after, before)
	return args.Get(0).([]File), args.Error(1)
}

type mockOutbox struct {
	mock.Mock
}

func (m *mockOutbox) Insert(ctx context.Context, event OutboxEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

type mockTransactor struct {
	mock.Mock
}

func (m *mockTransactor) WithinTx(ctx context.Context, txFunc func(txCtx context.Context) error) error {
	m.Called(ctx)
	return txFunc(ctx)
}

func newService(store ObjectStore, repo Repository) *service {
	outbox := new(mockOutbox)
	outbox.On("Insert", mock.Anything, mock.Anything).Return(nil)
	tx := new(mockTransactor)
	tx.On("WithinTx", mock.Anything).Return(nil)
	return newServiceTx(store, repo, outbox, tx)
}

func newServiceTx(store ObjectStore, repo Repository, outbox OutboxRepository, tx Transactor) *service {
	return New(store, repo, outbox, tx, testRootDir, testPendingFileTTL).(*service)
}

func testCtx() context.Context {
	return logger.WithCtx(context.Background(), logger.NewLogger(false))
}

func validInput() StoreInput {
	return StoreInput{UserID: 7, Checksum: strings.Repeat("a", 64), Body: strings.NewReader("x")}
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

	var calls []string
	var gotKey string
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.MatchedBy(func(p PutInput) bool {
		return p.Size == in.Size &&
			p.ContentType == in.ContentType &&
			hex.EncodeToString(p.ChecksumSHA256) == in.Checksum
	})).
		Run(func(args mock.Arguments) {
			calls = append(calls, "Put")
			put := args.Get(1).(PutInput)
			gotKey = put.Key
			b, err := io.ReadAll(put.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(nil)

	var gotFile File
	var gotStatusID string
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			calls = append(calls, "Insert")
			gotFile = args.Get(1).(File)
		}).
		Return(File{Status: StatusPending}, nil)
	repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).
		Run(func(args mock.Arguments) {
			calls = append(calls, "UpdateStatus")
			gotStatusID = args.String(1)
		}).
		Return(File{Status: StatusUploaded, CreatedAt: createdAt}, nil)

	res, err := newService(store, repo).Store(testCtx(), in)

	require.NoError(t, err)
	_, err = uuid.Parse(res.UUID)
	require.NoError(t, err)
	require.Equal(t, "7/"+res.UUID, gotKey)
	require.Equal(t, res.UUID, gotStatusID)
	require.Equal(t, in.Filename, res.Filename)
	require.Equal(t, in.ContentType, res.ContentType)
	require.Equal(t, in.Size, res.Size)
	require.Equal(t, createdAt, res.CreatedAt, "CreatedAt must come from the row returned by the status update")
	require.Equal(t, File{
		ID:             res.UUID,
		UserID:         7,
		Filename:       in.Filename,
		ContentType:    in.ContentType,
		Path:           gotKey,
		RootDir:        testRootDir,
		Size:           in.Size,
		ChecksumSHA256: sum[:],
		Status:         StatusPending,
	}, gotFile)
	require.Equal(t, []string{"Insert", "Put", "UpdateStatus"}, calls,
		"the pending record must exist before the upload and be marked uploaded only afterwards")
	repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
	store.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestService_Store_GeneratesUniqueUUIDs(t *testing.T) {
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).Return(File{}, nil)
	svc := newService(store, repo)

	first, err := svc.Store(testCtx(), validInput())
	require.NoError(t, err)
	second, err := svc.Store(testCtx(), validInput())
	require.NoError(t, err)

	require.NotEqual(t, first.UUID, second.UUID)
}

func TestService_Store_RejectsMalformedChecksum(t *testing.T) {
	store := new(mockObjectStore)
	repo := new(mockRepository)

	_, err := newService(store, repo).Store(testCtx(), StoreInput{Checksum: "not-hex", Body: strings.NewReader("x")})

	require.ErrorIs(t, err, ErrInvalidChecksum)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Insert", mock.Anything, mock.Anything)
}

func TestService_Store_DuplicateFileSkipsUpload(t *testing.T) {
	store := new(mockObjectStore)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, ErrDuplicateFile)

	res, err := newService(store, repo).Store(testCtx(), validInput())

	require.ErrorIs(t, err, ErrDuplicateFile)
	require.Equal(t, StoreResult{}, res)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

func TestService_Store_InsertErrorSkipsUpload(t *testing.T) {
	boom := errors.New("db down")
	store := new(mockObjectStore)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, boom)

	res, err := newService(store, repo).Store(testCtx(), validInput())

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

func TestService_Store_PutErrorDeletesRecord(t *testing.T) {
	boom := errors.New("storage unreachable")
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(boom)

	var insertedID string
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { insertedID = args.Get(1).(File).ID }).
		Return(File{}, nil)
	var deletedID string
	repo.On("Delete", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { deletedID = args.String(1) }).
		Return(nil)

	res, err := newService(store, repo).Store(testCtx(), validInput())

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	require.NotEmpty(t, insertedID)
	require.Equal(t, insertedID, deletedID, "the record created for this upload must be the one deleted")
	repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
	store.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestService_Store_PutErrorStillReturnsPutErrorWhenDeleteFails(t *testing.T) {
	putErr := errors.New("storage unreachable")
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(putErr)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	repo.On("Delete", mock.Anything, mock.Anything).Return(errors.New("db down"))

	res, err := newService(store, repo).Store(testCtx(), validInput())

	require.ErrorIs(t, err, putErr)
	require.Equal(t, StoreResult{}, res)
	repo.AssertExpectations(t)
}

func TestService_Store_CleanupRunsAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx())
	cancel()

	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(context.Canceled)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	repo.On("Delete", mock.MatchedBy(func(c context.Context) bool {
		_, hasDeadline := c.Deadline()
		return c.Err() == nil && hasDeadline
	}), mock.Anything).Return(nil)

	_, err := newService(store, repo).Store(ctx, validInput())

	require.ErrorIs(t, err, context.Canceled)
	repo.AssertExpectations(t)
}

func TestService_Store_UpdateStatusErrorIsReturned(t *testing.T) {
	boom := errors.New("db down")
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).Return(File{}, boom)

	res, err := newService(store, repo).Store(testCtx(), validInput())

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
	store.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestService_Store_RecordsOutboxEventInTransaction(t *testing.T) {
	var calls []string
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{ID: "file-1", Status: StatusPending}, nil)
	repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).
		Run(func(mock.Arguments) { calls = append(calls, "UpdateStatus") }).
		Return(File{Status: StatusUploaded}, nil)
	var gotEvent OutboxEvent
	outbox := new(mockOutbox)
	outbox.On("Insert", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			calls = append(calls, "OutboxInsert")
			gotEvent = args.Get(1).(OutboxEvent)
		}).
		Return(nil)
	tx := new(mockTransactor)
	tx.On("WithinTx", mock.Anything).
		Run(func(mock.Arguments) { calls = append(calls, "Begin") }).
		Return(nil)

	res, err := newServiceTx(store, repo, outbox, tx).Store(testCtx(), validInput())

	require.NoError(t, err)
	require.Equal(t, []string{"Begin", "UpdateStatus", "OutboxInsert"}, calls)
	require.Equal(t, "file-1", gotEvent.AggregateID)
	require.Equal(t, events.AggregateTypeFile, gotEvent.AggregateType)
	require.Equal(t, events.EventTypeFileUploaded, gotEvent.EventType)
	require.NotEmpty(t, res.UUID)
	_, err = uuid.Parse(gotEvent.ID)
	require.NoError(t, err)
	tx.AssertExpectations(t)
	outbox.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestService_Store_OutboxErrorIsReturned(t *testing.T) {
	boom := errors.New("db down")
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).Return(File{}, nil)
	outbox := new(mockOutbox)
	outbox.On("Insert", mock.Anything, mock.Anything).Return(boom)
	tx := new(mockTransactor)
	tx.On("WithinTx", mock.Anything).Return(nil)

	res, err := newServiceTx(store, repo, outbox, tx).Store(testCtx(), validInput())

	require.ErrorIs(t, err, boom)
	require.Equal(t, StoreResult{}, res)
	repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
	outbox.AssertExpectations(t)
}

func TestService_Store_UpdateStatusErrorSkipsOutbox(t *testing.T) {
	store := new(mockObjectStore)
	store.On("Put", mock.Anything, mock.Anything).Return(nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(File{}, nil)
	repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).Return(File{}, errors.New("db down"))
	outbox := new(mockOutbox)
	tx := new(mockTransactor)
	tx.On("WithinTx", mock.Anything).Return(nil)

	_, err := newServiceTx(store, repo, outbox, tx).Store(testCtx(), validInput())

	require.Error(t, err)
	outbox.AssertNotCalled(t, "Insert", mock.Anything, mock.Anything)
}

func pendingFile(id string, createdAt time.Time) File {
	return File{ID: id, Path: "7/" + id, Status: StatusPending, CreatedAt: createdAt}
}

func TestService_CleanupPendingFiles(t *testing.T) {
	t0 := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("queries from the zero time up to now minus the TTL", func(t *testing.T) {
		before := time.Now()
		var gotAfter, gotBefore time.Time
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				gotAfter = args.Get(1).(time.Time)
				gotBefore = args.Get(2).(time.Time)
			}).
			Return([]File{}, nil)

		err := newService(new(mockObjectStore), repo).CleanupPendingFiles(testCtx())
		after := time.Now()

		require.NoError(t, err)
		require.True(t, gotAfter.IsZero())
		require.False(t, gotBefore.Before(before.Add(-testPendingFileTTL)))
		require.False(t, gotBefore.After(after.Add(-testPendingFileTTL)))
		repo.AssertExpectations(t)
	})

	t.Run("marks files that exist in the object store as uploaded", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, "7/a").Return(true, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0)}, nil)
		repo.On("UpdateStatus", mock.Anything, "a", StatusUploaded).Return(File{}, nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
		store.AssertExpectations(t)
		repo.AssertExpectations(t)
	})

	t.Run("deletes records of files missing from the object store", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, "7/a").Return(false, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0)}, nil)
		repo.On("Delete", mock.Anything, "a").Return(nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
		store.AssertExpectations(t)
		repo.AssertExpectations(t)
	})

	t.Run("skips a file whose existence check fails and keeps processing the rest", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, "7/a").Return(false, errors.New("s3 unreachable"))
		store.On("Exists", mock.Anything, "7/b").Return(false, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0), pendingFile("b", t0.Add(time.Second))}, nil)
		repo.On("Delete", mock.Anything, "b").Return(nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertNotCalled(t, "Delete", mock.Anything, "a")
		repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, "a", mock.Anything)
		repo.AssertExpectations(t)
	})

	t.Run("skips a file whose status update fails and keeps processing the rest", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, mock.Anything).Return(true, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0), pendingFile("b", t0.Add(time.Second))}, nil)
		repo.On("UpdateStatus", mock.Anything, "a", StatusUploaded).Return(File{}, errors.New("db down"))
		repo.On("UpdateStatus", mock.Anything, "b", StatusUploaded).Return(File{}, nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("records an outbox event in the same transaction when marking a file as uploaded", func(t *testing.T) {
		var calls []string
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, "7/a").Return(true, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0)}, nil)
		repo.On("UpdateStatus", mock.Anything, "a", StatusUploaded).
			Run(func(mock.Arguments) { calls = append(calls, "UpdateStatus") }).
			Return(File{}, nil)
		var gotEvent OutboxEvent
		outbox := new(mockOutbox)
		outbox.On("Insert", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				calls = append(calls, "OutboxInsert")
				gotEvent = args.Get(1).(OutboxEvent)
			}).
			Return(nil)
		tx := new(mockTransactor)
		tx.On("WithinTx", mock.Anything).
			Run(func(mock.Arguments) { calls = append(calls, "Begin") }).
			Return(nil)

		err := newServiceTx(store, repo, outbox, tx).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		require.Equal(t, []string{"Begin", "UpdateStatus", "OutboxInsert"}, calls)
		require.Equal(t, "a", gotEvent.AggregateID)
		require.Equal(t, events.EventTypeFileUploaded, gotEvent.EventType)
		tx.AssertExpectations(t)
		outbox.AssertExpectations(t)
	})

	t.Run("skips a file whose outbox insert fails and keeps processing the rest", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, mock.Anything).Return(true, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0), pendingFile("b", t0.Add(time.Second))}, nil)
		repo.On("UpdateStatus", mock.Anything, mock.Anything, StatusUploaded).Return(File{}, nil)
		outbox := new(mockOutbox)
		outbox.On("Insert", mock.Anything, mock.MatchedBy(func(e OutboxEvent) bool { return e.AggregateID == "a" })).
			Return(errors.New("db down"))
		outbox.On("Insert", mock.Anything, mock.MatchedBy(func(e OutboxEvent) bool { return e.AggregateID == "b" })).
			Return(nil)
		tx := new(mockTransactor)
		tx.On("WithinTx", mock.Anything).Return(nil)

		err := newServiceTx(store, repo, outbox, tx).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		outbox.AssertExpectations(t)
		repo.AssertNumberOfCalls(t, "UpdateStatus", 2)
	})

	t.Run("skips a file whose delete fails and keeps processing the rest", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0), pendingFile("b", t0.Add(time.Second))}, nil)
		repo.On("Delete", mock.Anything, "a").Return(errors.New("db down"))
		repo.On("Delete", mock.Anything, "b").Return(nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("passes repository errors through", func(t *testing.T) {
		boom := errors.New("db down")
		store := new(mockObjectStore)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).Return([]File{}, boom)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.ErrorIs(t, err, boom)
		store.AssertNotCalled(t, "Exists", mock.Anything, mock.Anything)
		repo.AssertExpectations(t)
	})

	t.Run("makes a single query when the batch is not full", func(t *testing.T) {
		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, mock.Anything, mock.Anything).
			Return([]File{pendingFile("a", t0)}, nil)
		repo.On("Delete", mock.Anything, mock.Anything).Return(nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertNumberOfCalls(t, "GetPendingFilesOlderThan", 1)
	})

	t.Run("pages through full batches using the last created_at as the cursor", func(t *testing.T) {
		firstBatch := make([]File, pendingBatchSize)
		for i := range firstBatch {
			firstBatch[i] = pendingFile(uuid.NewString(), t0.Add(time.Duration(i)*time.Second))
		}
		cursor := firstBatch[len(firstBatch)-1].CreatedAt
		secondBatch := []File{pendingFile("last", cursor.Add(time.Second))}

		store := new(mockObjectStore)
		store.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
		repo := new(mockRepository)
		repo.On("GetPendingFilesOlderThan", mock.Anything, time.Time{}, mock.Anything).Return(firstBatch, nil).Once()
		repo.On("GetPendingFilesOlderThan", mock.Anything, cursor, mock.Anything).Return(secondBatch, nil).Once()
		repo.On("Delete", mock.Anything, mock.Anything).Return(nil)

		err := newService(store, repo).CleanupPendingFiles(testCtx())

		require.NoError(t, err)
		repo.AssertNumberOfCalls(t, "GetPendingFilesOlderThan", 2)
		repo.AssertNumberOfCalls(t, "Delete", pendingBatchSize+1)
		repo.AssertExpectations(t)
	})
}
