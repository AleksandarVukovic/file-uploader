//go:build integration

package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/internal/objectstore"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newFileServiceServer(t *testing.T, s3API *mockS3PutObjectAPI) *httptest.Server {
	t.Helper()

	return newFileServiceServerWithRepo(t, s3API, newAcceptingRepository())
}

func newFileServiceServerWithRepo(t *testing.T, s3API *mockS3PutObjectAPI, repo storage.Repository) *httptest.Server {
	t.Helper()

	filesHandler := NewFilesHandler(storage.New(objectstore.NewS3("test-bucket", s3API), repo, "test-bucket"))
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
	req.Header.Set("X-User-Id", "1")
	return req
}

func TestUploadEndpoint_Success(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	var gotKey string
	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.MatchedBy(func(in *s3.PutObjectInput) bool {
		return aws.ToString(in.Bucket) == "test-bucket" &&
			aws.ToString(in.ContentType) == "text/csv"
	}), mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(*s3.PutObjectInput)
			gotKey = aws.ToString(in.Key)
			b, err := io.ReadAll(in.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(&s3.PutObjectOutput{}, nil)

	var gotFile storage.File
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { gotFile = args.Get(1).(storage.File) }).
		Return(storage.File{}, nil)

	srv := newFileServiceServerWithRepo(t, m, repo)
	defer srv.Close()

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var got struct {
		UUID        string `json:"uuid"`
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		Size        int    `json:"size"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	_, err = uuid.Parse(got.UUID)
	require.NoError(t, err)
	require.Equal(t, "1/"+got.UUID, gotKey)
	require.Equal(t, "users.csv", got.Filename)
	require.Equal(t, "text/csv", got.ContentType)
	require.Equal(t, len(body), got.Size)
	require.Equal(t, storage.File{
		ID:             got.UUID,
		UserID:         1,
		Filename:       "users.csv",
		ContentType:    "text/csv",
		Path:           gotKey,
		RootDir:        "test-bucket",
		Size:           int64(len(body)),
		ChecksumSHA256: mustDecodeHex(t, checksum),
	}, gotFile)
	m.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestUploadEndpoint_DatabaseFailureMapsToInternalError(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)
	repo := new(mockRepository)
	repo.On("Insert", mock.Anything, mock.Anything).Return(storage.File{}, errors.New("db down"))

	srv := newFileServiceServerWithRepo(t, m, repo)
	defer srv.Close()

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	repo.AssertExpectations(t)
}

func TestUploadEndpoint_S3FailureMapsToInternalError(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("s3 unreachable"))
	repo := new(mockRepository)

	srv := newFileServiceServerWithRepo(t, m, repo)
	defer srv.Close()

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	repo.AssertNotCalled(t, "Insert", mock.Anything, mock.Anything)
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()

	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
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
