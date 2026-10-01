//go:build integration

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/api-service/internal/fileservice"
	"github.com/aleksandarv/file-uploader/common/http/client"
	"github.com/aleksandarv/file-uploader/common/logger"
	fsfiles "github.com/aleksandarv/file-uploader/file-service/gen/files"
	fsserver "github.com/aleksandarv/file-uploader/file-service/gen/http/files/server"
	"github.com/stretchr/testify/require"
)

func loadTestdata(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

func newAPIServer(t *testing.T, fileServiceURL string) *httptest.Server {
	t.Helper()

	fsURL, err := url.Parse(fileServiceURL)
	require.NoError(t, err)

	fsClient := fileservice.NewClient(fsURL.Scheme, fsURL.Host, false, client.NewDoer(false))
	filesSvc := NewFilesSvc(fsClient)
	handler := Routes(logger.NewLogger(false), filesSvc, NewHealthSvc())

	return httptest.NewServer(handler)
}

func newUploadRequest(t *testing.T, baseURL, filename, contentType, checksum, body string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/files/upload", strings.NewReader(body))
	require.NoError(t, err)

	req.Header.Set("Content-Disposition", filename)
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Checksum-Sha256", checksum)
	return req
}

func TestUploadEndpoint_Success(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	var (
		gotFilename string
		gotSize     string
		gotChecksum string
		gotBody     []byte
	)
	fakeFileService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilename = r.Header.Get("Content-Disposition")
		gotSize = r.Header.Get("X-File-Size")
		gotChecksum = r.Header.Get("X-Checksum-Sha256")
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		gotBody = b
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fakeFileService.Close()

	apiSrv := newAPIServer(t, fakeFileService.URL)
	defer apiSrv.Close()

	req := newUploadRequest(t, apiSrv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "users.csv", gotFilename)
	require.Equal(t, strconv.Itoa(len(body)), gotSize)
	require.Equal(t, checksum, gotChecksum)
	require.Equal(t, body, string(gotBody))
}

func TestUploadEndpoint_FileServiceRejectsBadRequest(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users.csv")
	checksum := checksumOf(body)

	fakeFileService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		svcErr := fsfiles.MakeBadRequest(errors.New("File size does not match declared size"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("goa-error", svcErr.GoaErrorName())
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(fsserver.NewUploadBadRequestResponseBody(svcErr))
	}))
	defer fakeFileService.Close()

	apiSrv := newAPIServer(t, fakeFileService.URL)
	defer apiSrv.Close()

	req := newUploadRequest(t, apiSrv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestUploadEndpoint_RejectsOversizedDeclaredSize(t *testing.T) {
	t.Parallel()

	body := loadTestdata(t, "users_large.csv")
	checksum := checksumOf(body)

	fileServiceCalled := false
	fakeFileService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileServiceCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fakeFileService.Close()

	apiSrv := newAPIServer(t, fakeFileService.URL)
	defer apiSrv.Close()

	req := newUploadRequest(t, apiSrv.URL, "users.csv", "text/csv", checksum, body)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.False(t, fileServiceCalled, "file-service should never be invoked for an oversized declared upload")

	var errResp struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
	require.Contains(t, errResp.Message, "size must be lesser or equal than",
		"rejection must specifically be for exceeding the max size, not some other validation failure")
	require.Contains(t, errResp.Message, "got value 10486784")
}
