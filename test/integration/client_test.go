//go:build e2e

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	authsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/auth/server"
	filessvr "github.com/aleksandarv/file-uploader/api-service/gen/http/files/server"
	. "github.com/onsi/gomega"
)

var (
	loginPath    = authsvr.LoginAuthPath()
	registerPath = authsvr.RegisterAuthPath()
	uploadPath   = filessvr.UploadFilesPath()
)

func checksumOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func loadTestdata(name string) string {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

func login(username, password string) (*http.Response, error) {
	payload, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, apiBaseURL+loginPath, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func register(username, email, password string) (*http.Response, error) {
	payload, err := json.Marshal(map[string]string{"username": username, "email": email, "password": password})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, apiBaseURL+registerPath, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func mustLoginToken() string {
	resp, err := login(e2eUsername, e2ePassword)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))

	var body struct {
		Token string `json:"token"`
	}
	Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
	Expect(body.Token).NotTo(BeEmpty())
	return body.Token
}

func newUploadRequest(filename, contentType, checksum, body, token string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, apiBaseURL+uploadPath, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Disposition", filename)
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Checksum-Sha256", checksum)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}
