//go:build e2e

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Upload", func() {
	It("stores the uploaded file in S3 through the full api-service -> file-service -> S3 path", func() {
		token := mustLoginToken()
		body := loadTestdata("users.csv")
		checksum := checksumOf(body)

		req, err := newUploadRequest("e2e-upload.csv", "text/csv", checksum, body, token)
		Expect(err).NotTo(HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var uploaded struct {
			UUID        string `json:"uuid"`
			Filename    string `json:"filename"`
			ContentType string `json:"contentType"`
			Size        int    `json:"size"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&uploaded)).To(Succeed())
		Expect(uploaded.UUID).NotTo(BeEmpty())
		Expect(uploaded.Filename).To(Equal("e2e-upload.csv"))
		Expect(uploaded.ContentType).To(Equal("text/csv"))
		Expect(uploaded.Size).To(Equal(len(body)))

		out, err := s3Client.GetObject(context.Background(), &s3.GetObjectInput{
			Bucket: aws.String(e2eS3Bucket),
			Key:    aws.String("e2e-upload.csv"),
		})
		Expect(err).NotTo(HaveOccurred())
		defer out.Body.Close()

		stored, err := io.ReadAll(out.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(stored)).To(Equal(body))
	})
})
