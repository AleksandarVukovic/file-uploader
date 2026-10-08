package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/tls"
	"github.com/aleksandarv/file-uploader/file-service/internal/api"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsc "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	var (
		debug         bool
		healthPort    int
		mtlsPort      int
		s3BucketName  string
		s3Endpoint    string
		tlsCertFile   string
		tlsKeyFile    string
		tlsCACertFile string
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&healthPort, "healthPort", 8080, "HTTP port for the health endpoint")
	flag.IntVar(&mtlsPort, "mtlsPort", 8443, "mTLS port for the internal files API")
	flag.StringVar(&s3BucketName, "bucketName", "", "Name of the S3 bucket where files will be saved")
	flag.StringVar(&s3Endpoint, "s3Endpoint", "", "Override the S3 endpoint (e.g. for LocalStack); leave empty to use AWS's default resolver")
	flag.StringVar(&tlsCertFile, "tlsCertFile", "", "Path to this service's TLS certificate")
	flag.StringVar(&tlsKeyFile, "tlsKeyFile", "", "Path to this service's TLS private key")
	flag.StringVar(&tlsCACertFile, "tlsCACertFile", "", "Path to the CA certificate used to verify client certificates")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	cfg, err := awsc.LoadDefaultConfig(ctx)
	if err != nil {
		log.Error("failed to load AWS config", "err", err)
		return err
	}
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if s3Endpoint != "" {
			o.BaseEndpoint = aws.String(s3Endpoint)
			o.UsePathStyle = true
			if isPlainHTTP(s3Endpoint) {
				o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
			}
		}
	})

	filesService := api.NewFilesHandler(s3BucketName, s3Client)

	tlsCfg, err := tls.NewServerConfig(tlsCertFile, tlsKeyFile, tlsCACertFile)
	if err != nil {
		log.Error("failed to build TLS config", "err", err)
		return err
	}

	healthAddr := ":" + strconv.Itoa(healthPort)
	healthSrv := &http.Server{
		Addr:              healthAddr,
		Handler:           api.HealthRoutes(log, api.NewHealthHandler()),
		ReadHeaderTimeout: time.Second * 60,
	}

	mtlsAddr := ":" + strconv.Itoa(mtlsPort)
	filesSrv := &http.Server{
		Addr:              mtlsAddr,
		Handler:           api.Routes(log, filesService),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: time.Second * 60,
	}

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

	go func() {
		log.Info("Start mTLS files server on", "host", mtlsAddr)
		errc <- filesSrv.ListenAndServeTLS("", "")
	}()

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		<-ctx.Done()
		log.Info("application shutdown requested, shutting down gracefully")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := healthSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("error while shutting down health server", "error", err)
		}
		if err := filesSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("error while shutting down mTLS files server", "error", err)
		}
		log.Info("application shutdown completed")
	})

	// waiting on some signal to shutdown the application
	err = <-errc
	if err != nil {
		log.Error("server stopped unexpectedly", "err", err)
	}
	log.Info("application exiting")

	// trigger shutdown goroutine process
	cancel()
	wg.Wait()
	return err
}

func isPlainHTTP(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "http"
}

func loadFlagsFromEnv() {
	envToFlag := map[string]string{
		"DEBUG":            "debug",
		"HEALTH_PORT":      "healthPort",
		"MTLS_PORT":        "mtlsPort",
		"S3_BUCKET_NAME":   "bucketName",
		"S3_ENDPOINT":      "s3Endpoint",
		"TLS_CERT_FILE":    "tlsCertFile",
		"TLS_KEY_FILE":     "tlsKeyFile",
		"TLS_CA_CERT_FILE": "tlsCACertFile",
	}
	for env, flagName := range envToFlag {
		if val := os.Getenv(env); val != "" {
			os.Args = append(os.Args, fmt.Sprintf("--%s=%s", flagName, val))
		}
	}
}
