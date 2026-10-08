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

const testUUID = "0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11"

type mockFileService struct {
	mock.Mock
}

func (m *mockFileService) Upload(ctx context.Context, p *fsfiles.UploadPayload, body io.ReadCloser) (*fsfiles.UploadResult, error) {
	args := m.Called(ctx, p, body)
	return args.Get(0).(*fsfiles.UploadResult), args.Error(1)
}

func expectedDownstreamPayload(p *files.UploadPayload) *fsfiles.UploadPayload {
	return &fsfiles.UploadPayload{
		Filename:    p.Filename,
		Size:        p.Size,
		ContentType: p.ContentType,
		Checksum:    p.Checksum,
	}
}

func TestFilesHandler_Upload_Success(t *testing.T) {
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
		Return(&fsfiles.UploadResult{UUID: testUUID, Filename: payload.Filename, ContentType: payload.ContentType, Size: payload.Size}, nil)

	h := NewFilesHandler(mockfs)
	res, err := h.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.NoError(t, err)
	require.Equal(t, &files.UploadResult{UUID: testUUID, Filename: payload.Filename, ContentType: payload.ContentType, Size: payload.Size}, res)
	mockfs.AssertExpectations(t)
}

func TestFilesHandler_Upload_ForwardsBadRequestFromFileService(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	mockfs := new(mockFileService)
	mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).
		Return((*fsfiles.UploadResult)(nil), fsfiles.MakeBadRequest(errors.New("File size does not match declared size")))

	h := NewFilesHandler(mockfs)
	_, err := h.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.Error(t, err)

	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr))
	require.Equal(t, "bad_request", svcErr.Name)
	require.Equal(t, "File size does not match declared size", svcErr.Message)
	mockfs.AssertExpectations(t)
}

func TestFilesHandler_Upload_WrapsOtherFileServiceErrorsAsInternalError(t *testing.T) {
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
				Return((*fsfiles.UploadResult)(nil), tt.err)

			h := NewFilesHandler(mockfs)
			_, err := h.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

			require.Error(t, err)

			var svcErr *goa.ServiceError
			require.True(t, errors.As(err, &svcErr))
			require.Equal(t, "internal_error", svcErr.Name)
			require.Equal(t, "Error while storing file", svcErr.Message)
			mockfs.AssertExpectations(t)
		})
	}
}

func TestFilesHandler_Upload_ClosesBody(t *testing.T) {
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
	mockfs.On("Upload", mock.Anything, expectedDownstreamPayload(payload), mock.Anything).Return(&fsfiles.UploadResult{}, nil)

	h := NewFilesHandler(mockfs)
	_, err := h.Upload(testCtx(), payload, rc)
	require.NoError(t, err)
	rc.AssertExpectations(t)
	mockfs.AssertExpectations(t)
}

func TestFilesHandler_Upload_ClosesBodyOnFileServiceFailure(t *testing.T) {
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
		Return((*fsfiles.UploadResult)(nil), errors.New("s3 unreachable"))

	h := NewFilesHandler(mockfs)
	_, err := h.Upload(testCtx(), payload, rc)
	require.Error(t, err)
	rc.AssertExpectations(t)
	mockfs.AssertExpectations(t)
}
