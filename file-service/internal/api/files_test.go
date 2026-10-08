package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpmiddleware "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
)

const testUserID int64 = 7

func testCtx() context.Context {
	ctx := logger.WithCtx(context.Background(), logger.NewLogger(false))
	return httpmiddleware.WithUserID(ctx, testUserID)
}

func checksumOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func loadTestdata(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

type mockReadCloser struct {
	io.Reader
	mock.Mock
}

func (m *mockReadCloser) Close() error {
	return m.Called().Error(0)
}

type mockS3PutObjectAPI struct {
	mock.Mock
}

func (m *mockS3PutObjectAPI) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	var out *s3.PutObjectOutput
	if o := args.Get(0); o != nil {
		out = o.(*s3.PutObjectOutput)
	}
	return out, args.Error(1)
}

type mockStorage struct {
	mock.Mock
}

func (m *mockStorage) Upload(ctx context.Context, in storage.UploadInput) (storage.UploadResult, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(storage.UploadResult), args.Error(1)
}

func uploadPayload(body, checksum string) *files.UploadPayload {
	return &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksum,
	}
}

func TestFilesHandler_Upload_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := uploadPayload(body, checksumOf(body))

	m := new(mockStorage)
	m.On("Upload", mock.Anything, mock.MatchedBy(func(in storage.UploadInput) bool {
		_, uuidErr := uuid.Parse(in.UUID)
		return in.UserID == testUserID &&
			uuidErr == nil &&
			in.Filename == payload.Filename &&
			in.Size == payload.Size &&
			in.ContentType == payload.ContentType &&
			in.Checksum == payload.Checksum
	})).
		Run(func(args mock.Arguments) {
			b, err := io.ReadAll(args.Get(1).(storage.UploadInput).Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(storage.UploadResult{Size: payload.Size}, nil)

	h := NewFilesHandler(m)
	_, err := h.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.NoError(t, err)
	m.AssertExpectations(t)
}

func TestFilesHandler_Upload_MapsInvalidChecksumToBadRequest(t *testing.T) {
	body := "id,name\n1,foo\n"

	m := new(mockStorage)
	m.On("Upload", mock.Anything, mock.Anything).Return(storage.UploadResult{}, storage.ErrInvalidChecksum)

	h := NewFilesHandler(m)
	_, err := h.Upload(testCtx(), uploadPayload(body, "not-hex"), io.NopCloser(strings.NewReader(body)))

	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr))
	require.Equal(t, "bad_request", svcErr.Name)
	m.AssertExpectations(t)
}

func TestFilesHandler_Upload_WrapsStorageErrorAsInternalError(t *testing.T) {
	body := "id,name\n1,foo\n"

	m := new(mockStorage)
	m.On("Upload", mock.Anything, mock.Anything).
		Return(storage.UploadResult{}, errors.New("storage unreachable"))

	h := NewFilesHandler(m)
	_, err := h.Upload(testCtx(), uploadPayload(body, checksumOf(body)), io.NopCloser(strings.NewReader(body)))

	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr))
	require.Equal(t, "internal_error", svcErr.Name)
	require.Equal(t, "Error while storing file", svcErr.Message)
	m.AssertExpectations(t)
}

func TestFilesHandler_Upload_ClosesBody(t *testing.T) {
	body := "id,name\n1,foo\n"

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	m := new(mockStorage)
	m.On("Upload", mock.Anything, mock.Anything).Return(storage.UploadResult{}, nil)

	h := NewFilesHandler(m)
	_, err := h.Upload(testCtx(), uploadPayload(body, checksumOf(body)), rc)
	require.NoError(t, err)
	rc.AssertExpectations(t)
}

func TestFilesHandler_Upload_ClosesBodyOnStorageFailure(t *testing.T) {
	body := "id,name\n1,foo\n"

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	m := new(mockStorage)
	m.On("Upload", mock.Anything, mock.Anything).
		Return(storage.UploadResult{}, errors.New("storage unreachable"))

	h := NewFilesHandler(m)
	_, err := h.Upload(testCtx(), uploadPayload(body, checksumOf(body)), rc)
	require.Error(t, err)
	rc.AssertExpectations(t)
}
