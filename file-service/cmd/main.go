package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aleksandarv/file-uploader/file-service/gen/http/files/server"
	"github.com/aleksandarv/file-uploader/file-service/internal/api"
	awsc "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/http/middleware"
)

func main() {
	var (
		debug        bool
		port         int
		s3BucketName string
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&port, "port", 8080, "HTTP port")
	flag.StringVar(&s3BucketName, "bucketName", "", "Name of the S3 bucket where files will be saved")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	cfg, err := awsc.LoadDefaultConfig(ctx)
	if err != nil {
		log.Error("failed to load AWS config", "err", err)
		os.Exit(1)
	}
	s3Client := s3.NewFromConfig(cfg)

	filesService := api.NewFilesService(s3BucketName, s3Client)

	mux := goahttp.NewMuxer()
	server := server.New(files.NewEndpoints(filesService), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)

	// TODO: validate that requestID is sent
	server.Use(logger.RequestMiddleware(log))
	server.Use(middleware.PopulateRequestContext())

	server.Mount(mux)
	for _, m := range server.Mounts {
		log.Debug("expose API", "verb", m.Verb, "path", m.Pattern, "method", m.Method)
	}

	addr := ":" + strconv.Itoa(port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: time.Second * 60}

	errc := make(chan error)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	go func() {
		log.Info("Start server on", "host", addr)
		errc <- srv.ListenAndServe()
	}()

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		<-ctx.Done()
		log.Info("application shutdown requested, shutting down gracefully")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Error("error while shutting down", "error", err)
		}
		log.Info("application shutdown completed")
	})

	// waiting on some signal to shutdown the application
	err = <-errc
	log.Info("application exiting", "reason", err)

	// trigger shutdown goroutine process
	cancel()
	wg.Wait()
}

func loadFlagsFromEnv() {
	envToFlag := map[string]string{
		"DEBUG":          "debug",
		"PORT":           "port",
		"S3_BUCKET_NAME": "bucketName",
	}
	for env, flagName := range envToFlag {
		if val := os.Getenv(env); val != "" {
			os.Args = append(os.Args, fmt.Sprintf("--%s=%s", flagName, val))
		}
	}
}
