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
	"sync"
	"syscall"
	"time"

	"github.com/aleksandarv/file-uploader/api-service/internal/api"
	"github.com/aleksandarv/file-uploader/api-service/internal/fileservice"
	"github.com/aleksandarv/file-uploader/api-service/internal/userservice"
	"github.com/aleksandarv/file-uploader/common/grpc/interceptor"
	"github.com/aleksandarv/file-uploader/common/http/client"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/tls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	var (
		debug           bool
		port            int
		fileServiceURL  string
		userServiceAddr string
		tlsCertFile     string
		tlsKeyFile      string
		tlsCACertFile   string
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&port, "port", 8080, "HTTP port")
	flag.StringVar(&fileServiceURL, "fileServiceURL", "https://localhost:8443", "URL of the file-service")
	flag.StringVar(&userServiceAddr, "userServiceAddr", "localhost:8443", "host:port of the user-service gRPC (mTLS) endpoint")
	flag.StringVar(&tlsCertFile, "tlsCertFile", "", "Path to this service's TLS client certificate")
	flag.StringVar(&tlsKeyFile, "tlsKeyFile", "", "Path to this service's TLS client private key")
	flag.StringVar(&tlsCACertFile, "tlsCACertFile", "", "Path to the CA certificate used to verify file-service's and user-service's server certificates")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		err := errors.New("JWT_SECRET environment variable is required")
		log.Error(err.Error())
		return err
	}

	fsURL, err := url.Parse(fileServiceURL)
	if err != nil {
		log.Error("invalid fileServiceURL", "error", err)
		return err
	}

	tlsCfg, err := tls.NewClientConfig(tlsCertFile, tlsKeyFile, tlsCACertFile)
	if err != nil {
		log.Error("failed to build TLS config", "error", err)
		return err
	}

	fsClient := fileservice.NewClient(fsURL.Scheme, fsURL.Host, debug, client.NewDoer(debug, tlsCfg))
	filesService := api.NewFilesSvc(fsClient)

	userConn, err := grpc.NewClient(userServiceAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithChainUnaryInterceptor(interceptor.ClientRequestID()),
	)
	if err != nil {
		log.Error("failed to create user-service client", "error", err)
		return err
	}
	defer userConn.Close()
	authService := api.NewAuthSvc([]byte(jwtSecret), userservice.NewClient(userConn))

	handler := api.Routes(log, []byte(jwtSecret), filesService, api.NewHealthSvc(), authService)

	addr := ":" + strconv.Itoa(port)
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
		// be aware that these timeouts are in correlation with max file size!
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errc := make(chan error, 2)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		sig := <-c
		log.Info("shutdown signal received", "signal", sig.String())
		errc <- nil
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
	if err != nil {
		log.Error("server stopped unexpectedly", "err", err)
	}
	log.Info("application exiting")

	// trigger shutdown goroutine process
	cancel()
	wg.Wait()
	return err
}

func loadFlagsFromEnv() {
	envToFlag := map[string]string{
		"DEBUG":             "debug",
		"PORT":              "port",
		"FILE_SERVICE_URL":  "fileServiceURL",
		"USER_SERVICE_ADDR": "userServiceAddr",
		"TLS_CERT_FILE":     "tlsCertFile",
		"TLS_KEY_FILE":      "tlsKeyFile",
		"TLS_CA_CERT_FILE":  "tlsCACertFile",
	}
	for env, flagName := range envToFlag {
		if val := os.Getenv(env); val != "" {
			os.Args = append(os.Args, fmt.Sprintf("--%s=%s", flagName, val))
		}
	}
}
