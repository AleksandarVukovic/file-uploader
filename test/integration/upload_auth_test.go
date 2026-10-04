//go:build e2e

package integration

import (
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func signedToken(method jwt.SigningMethod, secret, subject string, ttl time.Duration) string {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	signed, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
	if err != nil {
		panic(err)
	}
	return signed
}

var _ = Describe("Upload authorization", func() {
	DescribeTable("rejects an upload",
		func(token func() string) {
			body := loadTestdata("users.csv")
			checksum := checksumOf(body)

			req, err := newUploadRequest("e2e-upload-rejected.csv", "text/csv", checksum, body, token())
			Expect(err).NotTo(HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		},
		Entry("with no token", func() string { return "" }),
		Entry("with a malformed token", func() string { return "not-a-jwt" }),
		Entry("with a token signed by the wrong secret", func() string {
			return signedToken(jwt.SigningMethodHS256, "not-the-real-secret", e2eUsername, time.Hour)
		}),
		Entry("with a token signed using a disallowed algorithm", func() string {
			return signedToken(jwt.SigningMethodHS384, e2eJWTSecret, e2eUsername, time.Hour)
		}),
		Entry("with an expired token", func() string {
			return signedToken(jwt.SigningMethodHS256, e2eJWTSecret, e2eUsername, -time.Hour)
		}),
	)

	It("accepts a correctly signed, non-expired token (control for the rejection cases above)", func() {
		body := loadTestdata("users.csv")
		checksum := checksumOf(body)

		token := signedToken(jwt.SigningMethodHS256, e2eJWTSecret, e2eUsername, time.Hour)
		req, err := newUploadRequest("e2e-upload-auth-control.csv", "text/csv", checksum, body, token)
		Expect(err).NotTo(HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized))
	})
})
