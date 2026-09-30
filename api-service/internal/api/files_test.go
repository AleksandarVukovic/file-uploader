package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/api-service/gen/files"
	"github.com/aleksandarv/file-uploader/common/logger"
	fsfiles "github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
)

func testCtx() context.Context {
	return logger.WithCtx(context.Background(), logger.NewLogger(false))
}

func checksumOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

type mockReadCloser struct {
	io.Reader
	mock.Mock
}

func (m *mockReadCloser) Close() error {
	return m.Called().Error(0)
}

type mockFileService struct {
	mock.Mock
}

func (m *mockFileService) Upload(ctx context.Context, p *fsfiles.UploadPayload, body io.ReadCloser) error {
	return m.Called(ctx, p, body).Error(0)
}

func expectedDownstreamPayload(p *files.UploadPayload) *fsfiles.UploadPayload {
	return &fsfiles.UploadPayload{
		Filename:    p.Filename,
		Size:        p.Size,
		ContentType: p.ContentType,
		Checksum:    p.Checksum,
	}
}

func TestFilesService_Upload_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	mockfs := new(mockFileService)
	mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).
		Run(func(args mock.Arguments) {
			b, err := io.ReadAll(args.Get(2).(io.ReadCloser))
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(nil)

	svc := NewFilesSvc(mockfs)
	err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.NoError(t, err)
	mockfs.AssertExpectations(t)
}

func TestFilesService_Upload_ForwardsBadRequestFromFileService(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	mockfs := new(mockFileService)
	mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).
		Return(fsfiles.MakeBadRequest(errors.New("File size does not match declared size")))

	svc := NewFilesSvc(mockfs)
	err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.Error(t, err)

	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr))
	require.Equal(t, "bad_request", svcErr.Name)
	require.Equal(t, "File size does not match declared size", svcErr.Message)
	mockfs.AssertExpectations(t)
}

func TestFilesService_Upload_WrapsOtherFileServiceErrorsAsInternalError(t *testing.T) {
	body := "id,name\n1,foo\n"

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "plain error",
			err:  errors.New("s3 unreachable"),
		},
		{
			name: "non-bad-request service error",
			err:  fsfiles.MakeInternalError(errors.New("s3 put failed")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &files.UploadPayload{
				Filename:    "users.csv",
				Size:        int64(len(body)),
				ContentType: "text/csv",
				Checksum:    checksumOf(body),
			}

			mockfs := new(mockFileService)
			mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).
				Return(tt.err)

			svc := NewFilesSvc(mockfs)
			err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

			require.Error(t, err)

			var svcErr *goa.ServiceError
			require.True(t, errors.As(err, &svcErr))
			require.Equal(t, "internal_error", svcErr.Name)
			require.Equal(t, "Error while storing file", svcErr.Message)
			mockfs.AssertExpectations(t)
		})
	}
}

func TestFilesService_Upload_ClosesBody(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	mockfs := new(mockFileService)
	mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).Return(nil)

	svc := NewFilesSvc(mockfs)
	require.NoError(t, svc.Upload(testCtx(), payload, rc))
	rc.AssertExpectations(t)
	mockfs.AssertExpectations(t)
}

func TestFilesService_Upload_ClosesBodyOnFileServiceFailure(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	mockfs := new(mockFileService)
	mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).
		Return(errors.New("s3 unreachable"))

	svc := NewFilesSvc(mockfs)
	require.Error(t, svc.Upload(testCtx(), payload, rc))
	rc.AssertExpectations(t)
	mockfs.AssertExpectations(t)
}
