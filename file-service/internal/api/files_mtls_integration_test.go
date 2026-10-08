//go:build integration

package api

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	commontls "github.com/aleksandarv/file-uploader/common/tls"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func repoCertPath(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("..", "..", "..", "certs", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dev cert %q not found — run `make certs` from the repo root first: %v", path, err)
	}
	return path
}

func newMTLSFileServiceServer(t *testing.T, s3API *mockS3PutObjectAPI) *httptest.Server {
	t.Helper()

	tlsCfg, err := commontls.NewServerConfig(
		repoCertPath(t, "file-service.pem"),
		repoCertPath(t, "file-service-key.pem"),
		repoCertPath(t, "ca.pem"),
	)
	require.NoError(t, err)

	handler := Routes(logger.NewLogger(false), NewFilesHandler(storage.NewS3("test-bucket", s3API)))

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = tlsCfg
	srv.StartTLS()
	return srv
}

func trustedCAPool(t *testing.T) *x509.CertPool {
	t.Helper()

	caPEM, err := os.ReadFile(repoCertPath(t, "ca.pem"))
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))
	return pool
}

func TestMTLS_RejectsRequestWithoutClientCert(t *testing.T) {
	t.Parallel()

	srv := newMTLSFileServiceServer(t, new(mockS3PutObjectAPI))
	defer srv.Close()

	httpClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: trustedCAPool(t), ServerName: "file-service"},
	}}

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksumOf("x"), "x")
	_, err := httpClient.Do(req)
	require.Error(t, err, "a request without a client certificate must fail the TLS handshake")
}

func TestMTLS_RejectsClientCertFromDifferentCA(t *testing.T) {
	t.Parallel()

	srv := newMTLSFileServiceServer(t, new(mockS3PutObjectAPI))
	defer srv.Close()

	invalidCert, err := tls.LoadX509KeyPair(
		filepath.Join("testdata", "invalid-client-cert.pem"),
		filepath.Join("testdata", "invalid-client-key.pem"),
	)
	require.NoError(t, err)

	httpClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{invalidCert},
			RootCAs:      trustedCAPool(t),
			ServerName:   "file-service",
		},
	}}

	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksumOf("x"), "x")
	_, err = httpClient.Do(req)
	require.Error(t, err, "a client certificate signed by a CA the server doesn't trust must be rejected")
}

func TestMTLS_AcceptsRequestWithValidClientCert(t *testing.T) {
	t.Parallel()

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(&s3.PutObjectOutput{}, nil)

	srv := newMTLSFileServiceServer(t, m)
	defer srv.Close()

	tlsCfg, err := commontls.NewClientConfig(
		repoCertPath(t, "api-service.pem"),
		repoCertPath(t, "api-service-key.pem"),
		repoCertPath(t, "ca.pem"),
	)
	require.NoError(t, err)
	tlsCfg.ServerName = "file-service"

	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}

	body := loadTestdata(t, "users.csv")
	req := newUploadRequest(t, srv.URL, "users.csv", "text/csv", checksumOf(body), body)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	m.AssertExpectations(t)
}
