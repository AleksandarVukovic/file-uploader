//go:build integration

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newFileServiceServer(t *testing.T, s3API *mockS3PutObjectAPI) *httptest.Server {
	t.Helper()

	filesHandler := NewFilesHandler(storage.NewS3("test-bucket", s3API))
	handler := Routes(logger.NewLogger(false), filesHandler)

	return httptest.NewServer(handler)
}

func newUploadRequest(t *testing.T, baseURL, filename, contentType, checksum, body string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+filesUploadPath, strings.NewReader(body))
	require.NoError(t, err)

	req.Header.Set("Content-Disposition", filename)
	req.Header.Set("X-File-Size", strconv.Itoa(len(body)))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Checksum-Sha256", checksum)
	req.Header.Set("X-Request-Id", "test-request-id")
	return req
}

func TestUploadEndpoint_Success(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.MatchedBy(func(in *s3.PutObjectInput) bool {
		return aws.ToString(in.Bucket) == "test-bucket" &&
			aws.ToString(in.Key) == "users.csv" &&
			aws.ToString(in.ContentType) == "text/csv"
	}), mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(*s3.PutObjectInput)
			b, err := io.ReadAll(in.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(&s3.PutObjectOutput{}, nil)

	srv := newFileServiceServer(t, m)
	defer srv.Close()

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	m.AssertExpectations(t)
}

func TestUploadEndpoint_S3FailureMapsToInternalError(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("s3 unreachable"))

	srv := newFileServiceServer(t, m)
	defer srv.Close()

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestUploadEndpoint_RejectsOversizedDeclaredSize(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	m := new(mockS3PutObjectAPI)

	srv := newFileServiceServer(t, m)
	defer srv.Close()

	const declaredSize = 10485760 + 1

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksum, body)
	req.Header.Set("X-File-Size", strconv.Itoa(declaredSize))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	m.AssertNotCalled(t, "PutObject", mock.Anything, mock.Anything, mock.Anything)

	var errResp struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
	require.Contains(t, errResp.Message, "size must be lesser or equal than",
		"rejection must specifically be for exceeding the max size, not some other validation failure")
	require.Contains(t, errResp.Message, "got value "+strconv.Itoa(declaredSize))
}
