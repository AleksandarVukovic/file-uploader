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

// trackedCloser records whether Close was called, so tests can assert the
// service always closes the request body, including on validation failures.
type trackedCloser struct {
	io.Reader
	closed bool
}

func (c *trackedCloser) Close() error {
	c.closed = true
	return nil
}

func TestFilesService_Upload_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	svc := NewFilesService()
	err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.NoError(t, err)
}

func TestFilesService_Upload_BadRequest(t *testing.T) {
	body := "id,name\n1,foo\n"

	tests := []struct {
		name        string
		payload     *files.UploadPayload
		wantMessage string
	}{
		{
			name: "declared size larger than actual body",
			payload: &files.UploadPayload{
				Filename:    "users.csv",
				Size:        int64(len(body)) + 1,
				ContentType: "text/csv",
				Checksum:    checksumOf(body),
			},
			wantMessage: "File size does not match declared size",
		},
		{
			name: "declared size smaller than actual body",
			payload: &files.UploadPayload{
				Filename:    "users.csv",
				Size:        int64(len(body)) - 1,
				ContentType: "text/csv",
				Checksum:    checksumOf(body),
			},
			wantMessage: "File size does not match declared size",
		},
		{
			name: "checksum does not match body",
			payload: &files.UploadPayload{
				Filename:    "users.csv",
				Size:        int64(len(body)),
				ContentType: "text/csv",
				Checksum:    checksumOf("something else"),
			},
			wantMessage: "Uploaded file checksum does not match declared checksum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFilesService()
			err := svc.Upload(testCtx(), tt.payload, io.NopCloser(strings.NewReader(body)))

			require.Error(t, err)

			var svcErr *goa.ServiceError
			require.True(t, errors.As(err, &svcErr))
			require.Equal(t, "bad_request", svcErr.Name)
			require.Equal(t, tt.wantMessage, svcErr.Message)
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

	rc := &trackedCloser{Reader: strings.NewReader(body)}

	svc := NewFilesService()
	require.NoError(t, svc.Upload(testCtx(), payload, rc))
	require.True(t, rc.closed)
}

func TestFilesService_Upload_ClosesBodyOnValidationFailure(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf("mismatched"),
	}

	rc := &trackedCloser{Reader: strings.NewReader(body)}

	svc := NewFilesService()
	require.Error(t, svc.Upload(testCtx(), payload, rc))
	require.True(t, rc.closed)
}
