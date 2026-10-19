package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/messaging"
	"github.com/aleksandarv/file-uploader/common/telemetry"
	"github.com/aleksandarv/file-uploader/common/tls"
	"github.com/aleksandarv/file-uploader/file-processing-service/internal/api"
	"github.com/aleksandarv/file-uploader/file-processing-service/internal/objectstore"
	"github.com/aleksandarv/file-uploader/file-processing-service/internal/service/processor"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsc "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/tracing/smithyoteltracing"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
	"go.opentelemetry.io/otel"
)

const (
	serviceName          = "file-processing-service"
	brokerConfirmTimeout = 2 * time.Second
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	var (
		debug                bool
		healthPort           int
		s3BucketNames        string
		s3Endpoint           string
		tlsCACertFile        string
		brokerURL            string
		brokerFilesQueue     string
		brokerUsersExchange  string
		brokerTLSCertFile    string
		brokerTLSKeyFile     string
		otelCollectorURL     string
		otelTraceSampleRatio float64
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&healthPort, "healthPort", 8080, "HTTP port for the health endpoint")
	flag.StringVar(&s3BucketNames, "bucketNames", "", "Comma-separated names of the S3 buckets that uploaded files may be read from")
	flag.StringVar(&s3Endpoint, "s3Endpoint", "", "Override the S3 endpoint (e.g. for LocalStack); leave empty to use AWS's default resolver")
	flag.StringVar(&tlsCACertFile, "tlsCACertFile", "", "Path to the CA certificate used to verify the RabbitMQ server certificate")
	flag.StringVar(&brokerURL, "brokerURL", "", "RabbitMQ URL including the vhost (e.g. amqps://rabbitmq:5671/file-uploader)")
	flag.StringVar(&brokerFilesQueue, "brokerFilesQueue", "q.file.uploaded", "RabbitMQ queue that file events are consumed from")
	flag.StringVar(&brokerUsersExchange, "brokerUsersExchange", "ex.users", "RabbitMQ exchange that user events are published to")
	flag.StringVar(&brokerTLSCertFile, "brokerTLSCertFile", "", "Path to the client certificate presented to RabbitMQ (its CN is the RabbitMQ user)")
	flag.StringVar(&brokerTLSKeyFile, "brokerTLSKeyFile", "", "Path to the private key of the RabbitMQ client certificate")
	flag.StringVar(&otelCollectorURL, "otelCollectorURL", "", "URL of the OTEL Collector; telemetry is disabled when empty")
	flag.Float64Var(&otelTraceSampleRatio, "otelTraceSampleRatio", 1.0, "Fraction of new traces to sample, between 0 and 1")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	bucketNames := splitList(s3BucketNames)
	if len(bucketNames) == 0 {
		err := errors.New("at least one bucket name is required")
		log.Error("invalid bucketNames", "err", err)
		return err
	}

	telemetryShutdown, err := telemetry.Init(ctx, serviceName, otelCollectorURL, otelTraceSampleRatio)
	if err != nil {
		log.Error("failed to initialize OTEL", "error", err)
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := telemetryShutdown(shutdownCtx); err != nil {
			log.Error("error while shutting down OTEL providers", "error", err)
		}
	}()

	cfg, err := awsc.LoadDefaultConfig(ctx)
	if err != nil {
		log.Error("failed to load AWS config", "err", err)
		return err
	}
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.TracerProvider = smithyoteltracing.Adapt(otel.GetTracerProvider())
		if s3Endpoint != "" {
			o.BaseEndpoint = aws.String(s3Endpoint)
			o.UsePathStyle = true
			if isPlainHTTP(s3Endpoint) {
				o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
			}
		}
	})

	// do ping only for one s3 bucket in order to check if aws connection works
	pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = s3Client.HeadBucket(pingCtx, &s3.HeadBucketInput{Bucket: aws.String(bucketNames[0])})
	pingCancel()
	if err != nil {
		log.Error("failed to reach S3 bucket", "bucket", bucketNames[0], "err", err)
		return err
	}

	brokerTLSCfg, err := tls.NewClientConfig(brokerTLSCertFile, brokerTLSKeyFile, tlsCACertFile)
	if err != nil {
		log.Error("failed to build broker TLS config", "err", err)
		return err
	}
	brokerConn, err := rabbitmq.NewConn(brokerURL,
		rabbitmq.WithConnectionOptionsLogging,
		rabbitmq.WithConnectionOptionsConfig(rabbitmq.Config{
			SASL:            []amqp.Authentication{&amqp.ExternalAuth{}},
			TLSClientConfig: brokerTLSCfg,
		}),
	)
	if err != nil {
		log.Error("failed to connect to broker", "err", err)
		return err
	}
	defer brokerConn.Close()
	brokerPub, err := rabbitmq.NewPublisher(brokerConn, rabbitmq.WithPublisherOptionsConfirm)
	if err != nil {
		log.Error("failed to create broker publisher", "err", err)
		return err
	}
	defer brokerPub.Close()

	processorSvc := processor.New(
		log,
		objectstore.NewS3(bucketNames, s3Client),
		messaging.NewRabbitMQPublisher(brokerPub, brokerUsersExchange, brokerConfirmTimeout),
	)

	hostname, err := os.Hostname()
	if err != nil {
		log.Error("failed to get hostname", "err", err)
		return err
	}
	brokerConsumer, err := rabbitmq.NewConsumer(brokerConn, brokerFilesQueue,
		rabbitmq.WithConsumerOptionsQueueNoDeclare,
		rabbitmq.WithConsumerOptionsConcurrency(1),
		rabbitmq.WithConsumerOptionsQOSPrefetch(1),
		rabbitmq.WithConsumerOptionsConsumerName("file-processing-service-"+hostname),
	)
	if err != nil {
		log.Error("failed to create broker consumer", "err", err)
		return err
	}

	healthAddr := ":" + strconv.Itoa(healthPort)
	healthSrv := &http.Server{
		Addr:              healthAddr,
		Handler:           api.HealthRoutes(log, api.NewHealthHandler()),
		ReadHeaderTimeout: time.Second * 60,
	}

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	errc := make(chan error, 3)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		sig := <-c
		log.Info("shutdown signal received", "signal", sig.String())
		errc <- nil
	}()

	go func() {
		log.Info("Start health server on", "host", healthAddr)
		errc <- healthSrv.ListenAndServe()
	}()

	wg.Go(func() {
		log.Info("Start consuming file events from", "queue", brokerFilesQueue)
		if err := brokerConsumer.Run(processorSvc.Process); err != nil {
			errc <- err
		}
		log.Info("file events consumer stopped")
	})

	wg.Go(func() {
		<-ctx.Done()
		log.Info("application shutdown requested, shutting down gracefully")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		brokerConsumer.Close()
		if err := healthSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("error while shutting down health server", "error", err)
		}
		log.Info("application shutdown completed")
	})

	err = <-errc
	if err != nil {
		log.Error("server stopped unexpectedly", "err", err)
	}
	log.Info("application exiting")

	cancel()
	wg.Wait()
	return err
}

func splitList(s string) []string {
	var items []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func isPlainHTTP(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "http"
}

func loadFlagsFromEnv() {
	envToFlag := map[string]string{
		"DEBUG":                   "debug",
		"HEALTH_PORT":             "healthPort",
		"S3_BUCKET_NAMES":         "bucketNames",
		"S3_ENDPOINT":             "s3Endpoint",
		"TLS_CA_CERT_FILE":        "tlsCACertFile",
		"BROKER_URL":              "brokerURL",
		"BROKER_FILES_QUEUE":      "brokerFilesQueue",
		"BROKER_USERS_EXCHANGE":   "brokerUsersExchange",
		"BROKER_TLS_CERT_FILE":    "brokerTLSCertFile",
		"BROKER_TLS_KEY_FILE":     "brokerTLSKeyFile",
		"OTEL_COLLECTOR_URL":      "otelCollectorURL",
		"OTEL_TRACE_SAMPLE_RATIO": "otelTraceSampleRatio",
	}
	for env, flagName := range envToFlag {
		if val := os.Getenv(env); val != "" {
			os.Args = append(os.Args, fmt.Sprintf("--%s=%s", flagName, val))
		}
	}
}
