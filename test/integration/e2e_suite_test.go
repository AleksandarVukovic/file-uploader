//go:build e2e

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	tccompose "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	e2eUsername  = "e2e_user"
	e2eEmail     = "e2e.user@example.com"
	e2ePassword  = "E2e-Password-1"
	e2eJWTSecret = "e2e-test-secret"
	e2eS3Bucket  = "e2e-test-bucket"
	e2eAWSRegion = "us-east-1"
)

var (
	composeStack tccompose.ComposeStack
	apiBaseURL   string
	s3Client     *s3.Client
	e2eUserID    int64
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	for _, name := range []string{
		"api-service.pem", "api-service-key.pem",
		"file-service.pem", "file-service-key.pem",
		"user-service.pem", "user-service-key.pem",
		"user-db.pem", "user-db-key.pem",
		"ca.pem",
	} {
		_, err := os.Stat(filepath.Join("..", "..", "certs", name))
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("dev cert %q not found — run `make certs` from the repo root first", name))
	}

	ctx := context.Background()

	stack, err := tccompose.NewDockerCompose("docker-compose.e2e.yml")
	Expect(err).NotTo(HaveOccurred())
	composeStack = stack

	composeStack.
		WaitForService("localstack", wait.ForHealthCheck()).
		WaitForService("file-service", wait.ForHealthCheck()).
		WaitForService("user-service", wait.ForHealthCheck()).
		WaitForService("api-service", wait.ForHealthCheck())

	Expect(composeStack.Up(ctx, tccompose.Wait(true))).To(Succeed())

	apiContainer, err := composeStack.ServiceContainer(ctx, "api-service")
	Expect(err).NotTo(HaveOccurred())
	apiPort, err := apiContainer.MappedPort(ctx, "8080/tcp")
	Expect(err).NotTo(HaveOccurred())
	apiHost, err := apiContainer.Host(ctx)
	Expect(err).NotTo(HaveOccurred())
	apiBaseURL = fmt.Sprintf("http://%s:%s", apiHost, apiPort.Port())

	lsContainer, err := composeStack.ServiceContainer(ctx, "localstack")
	Expect(err).NotTo(HaveOccurred())
	lsPort, err := lsContainer.MappedPort(ctx, "4566/tcp")
	Expect(err).NotTo(HaveOccurred())
	lsHost, err := lsContainer.Host(ctx)
	Expect(err).NotTo(HaveOccurred())
	lsEndpoint := fmt.Sprintf("http://%s:%s", lsHost, lsPort.Port())

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(e2eAWSRegion),
		awsconfig.WithCredentialsProvider(aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
		})),
	)
	Expect(err).NotTo(HaveOccurred())

	s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(lsEndpoint)
		o.UsePathStyle = true
	})

	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(e2eS3Bucket)})
	Expect(err).NotTo(HaveOccurred())

	resp, err := register(e2eUsername, e2eEmail, e2ePassword)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))

	var registered struct {
		ID int64 `json:"id"`
	}
	Expect(json.NewDecoder(resp.Body).Decode(&registered)).To(Succeed())
	Expect(registered.ID).NotTo(BeZero())
	e2eUserID = registered.ID
})

var _ = AfterSuite(func() {
	if composeStack == nil {
		return
	}
	Expect(composeStack.Down(context.Background(),
		tccompose.RemoveOrphans(true),
		tccompose.RemoveVolumes(true),
		tccompose.RemoveImagesLocal,
	)).To(Succeed())
})
