//go:build e2e

package integration

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	e2eBrokerVHost   = "file-uploader"
	e2eFilesExchange = "ex.files"
	e2eUploadedQueue = "q.file.uploaded"
	e2eUploadedKey   = "file.uploaded"
)

func dialBrokerAsFileProcessingService() *amqp.Connection {
	certsDir := filepath.Join("..", "..", "certs")

	cert, err := tls.LoadX509KeyPair(
		filepath.Join(certsDir, "rabbitmq-file-processing-service.pem"),
		filepath.Join(certsDir, "rabbitmq-file-processing-service-key.pem"),
	)
	Expect(err).NotTo(HaveOccurred())

	caPEM, err := os.ReadFile(filepath.Join(certsDir, "ca.pem"))
	Expect(err).NotTo(HaveOccurred())
	pool := x509.NewCertPool()
	Expect(pool.AppendCertsFromPEM(caPEM)).To(BeTrue())

	conn, err := amqp.DialConfig(fmt.Sprintf("amqps://%s/%s", rabbitmqAddr, e2eBrokerVHost), amqp.Config{
		SASL: []amqp.Authentication{&amqp.ExternalAuth{}},
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			ServerName:   "localhost",
			MinVersion:   tls.VersionTLS13,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return conn
}

func nextUploadedEventFor(ch *amqp.Channel, fileID string) (amqp.Delivery, events.FileUploadedPayload, bool) {
	for {
		delivery, ok, err := ch.Get(e2eUploadedQueue, true)
		Expect(err).NotTo(HaveOccurred())
		if !ok {
			return amqp.Delivery{}, events.FileUploadedPayload{}, false
		}

		var payload events.FileUploadedPayload
		Expect(json.Unmarshal(delivery.Body, &payload)).To(Succeed())
		if payload.FileID == fileID {
			return delivery, payload, true
		}
	}
}

var _ = Describe("File events", func() {
	It("publishes a FileUploaded event to the broker for an uploaded file", func() {
		token := mustLoginToken()
		body := uniqueTestdata("users.csv")
		checksum := checksumOf(body)

		req, err := newUploadRequest("e2e-event.csv", "text/csv", checksum, body, token)
		Expect(err).NotTo(HaveOccurred())
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var uploaded struct {
			UUID string `json:"uuid"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&uploaded)).To(Succeed())
		Expect(uploaded.UUID).NotTo(BeEmpty())

		conn := dialBrokerAsFileProcessingService()
		defer conn.Close()
		ch, err := conn.Channel()
		Expect(err).NotTo(HaveOccurred())
		defer ch.Close()

		var (
			delivery amqp.Delivery
			payload  events.FileUploadedPayload
		)
		Eventually(func() bool {
			var found bool
			delivery, payload, found = nextUploadedEventFor(ch, uploaded.UUID)
			return found
		}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "no FileUploaded event for the upload reached the queue")

		_, err = uuid.Parse(delivery.MessageId)
		Expect(err).NotTo(HaveOccurred())
		Expect(delivery.Type).To(Equal(events.EventTypeFileUploaded))
		Expect(delivery.ContentType).To(Equal("application/json"))
		Expect(delivery.DeliveryMode).To(Equal(amqp.Persistent))
		Expect(delivery.Exchange).To(Equal(e2eFilesExchange))
		Expect(delivery.RoutingKey).To(Equal(e2eUploadedKey))

		Expect(payload).To(Equal(events.FileUploadedPayload{
			FileID:      uploaded.UUID,
			UserID:      e2eUserID,
			RootDir:     e2eS3Bucket,
			Path:        fmt.Sprintf("%d/%s", e2eUserID, uploaded.UUID),
			ContentType: "text/csv",
			Size:        int64(len(body)),
			Checksum:    checksum,
		}))
	})
})
