package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aleksandarv/file-uploader/common/db"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/telemetry"
	"github.com/aleksandarv/file-uploader/common/tls"
	"github.com/aleksandarv/file-uploader/user-service/internal/api"
	sqlc "github.com/aleksandarv/file-uploader/user-service/internal/repository/gen"
	usersrepo "github.com/aleksandarv/file-uploader/user-service/internal/repository/users"
	"github.com/aleksandarv/file-uploader/user-service/internal/service/importer"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	serviceName = "user-service"
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
		grpcPort             int
		tlsCertFile          string
		tlsKeyFile           string
		tlsCACertFile        string
		brokerURL            string
		brokerUsersQueue     string
		brokerTLSCertFile    string
		brokerTLSKeyFile     string
		otelCollectorURL     string
		otelTraceSampleRatio float64
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&healthPort, "healthPort", 8080, "HTTP port for the health endpoint")
	flag.IntVar(&grpcPort, "grpcPort", 8443, "mTLS port for the internal gRPC API")
	flag.StringVar(&tlsCertFile, "tlsCertFile", "", "Path to this service's TLS certificate")
	flag.StringVar(&tlsKeyFile, "tlsKeyFile", "", "Path to this service's TLS private key")
	flag.StringVar(&tlsCACertFile, "tlsCACertFile", "", "Path to the CA certificate used to verify client certificates and the RabbitMQ server certificate")
	flag.StringVar(&brokerURL, "brokerURL", "", "RabbitMQ URL including the vhost (e.g. amqps://rabbitmq:5671/file-uploader)")
	flag.StringVar(&brokerUsersQueue, "brokerUsersQueue", "q.user.imported", "RabbitMQ queue that user import events are consumed from")
	flag.StringVar(&brokerTLSCertFile, "brokerTLSCertFile", "", "Path to the client certificate presented to RabbitMQ (its CN is the RabbitMQ user)")
	flag.StringVar(&brokerTLSKeyFile, "brokerTLSKeyFile", "", "Path to the private key of the RabbitMQ client certificate")
	flag.StringVar(&otelCollectorURL, "otelCollectorURL", "", "URL of the OTEL Collector; telemetry is disabled when empty")
	flag.Float64Var(&otelTraceSampleRatio, "otelTraceSampleRatio", 1.0, "Fraction of new traces to sample, between 0 and 1")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	dbURL := os.Getenv("DATABASE_URL")
	if err := db.ValidateURL(dbURL); err != nil {
		log.Error("invalid DATABASE_URL", "err", err)
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

	pool, err := db.NewPool(ctx, dbURL)
	if err != nil {
		log.Error("failed to create database pool", "err", err)
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Error("failed to connect to database", "err", err)
		return err
	}

	repo := usersrepo.New(sqlc.New(pool))
	usersSvc := userssvc.New(repo)
	usersHandler := api.NewUsersHandler(usersSvc)

	tlsCfg, err := tls.NewServerConfig(tlsCertFile, tlsKeyFile, tlsCACertFile)
	if err != nil {
		log.Error("failed to build TLS config", "err", err)
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

	hostname, err := os.Hostname()
	if err != nil {
		log.Error("failed to get hostname", "err", err)
		return err
	}
	brokerConsumer, err := rabbitmq.NewConsumer(brokerConn, brokerUsersQueue,
		rabbitmq.WithConsumerOptionsQueueNoDeclare,
		rabbitmq.WithConsumerOptionsConcurrency(1),
		rabbitmq.WithConsumerOptionsQOSPrefetch(1),
		rabbitmq.WithConsumerOptionsConsumerName("user-service-"+hostname),
	)
	if err != nil {
		log.Error("failed to create broker consumer", "err", err)
		return err
	}
	importerSvc := importer.New(log, usersSvc)

	healthAddr := ":" + strconv.Itoa(healthPort)
	healthSrv := &http.Server{
		Addr:              healthAddr,
		Handler:           api.HealthRoutes(log, api.NewHealthHandler()),
		ReadHeaderTimeout: time.Second * 60,
	}

	grpcAddr := ":" + strconv.Itoa(grpcPort)
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("failed to listen for gRPC", "addr", grpcAddr, "err", err)
		return err
	}
	grpcSrv := api.GRPCServer(log, usersHandler, grpc.Creds(credentials.NewTLS(tlsCfg)))

	errc := make(chan error, 4)
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
		log.Info("Start mTLS gRPC server on", "host", grpcAddr)
		errc <- grpcSrv.Serve(grpcLis)
	}()

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		log.Info("Start consuming user import events from", "queue", brokerUsersQueue)
		if err := brokerConsumer.Run(importerSvc.Process); err != nil {
			errc <- err
		}
		log.Info("user import consumer stopped")
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

		stopped := make(chan struct{})
		go func() {
			grpcSrv.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-shutdownCtx.Done():
			log.Error("gRPC graceful stop timed out, forcing stop")
			grpcSrv.Stop()
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
		"DEBUG":                   "debug",
		"HEALTH_PORT":             "healthPort",
		"GRPC_PORT":               "grpcPort",
		"TLS_CERT_FILE":           "tlsCertFile",
		"TLS_KEY_FILE":            "tlsKeyFile",
		"TLS_CA_CERT_FILE":        "tlsCACertFile",
		"BROKER_URL":              "brokerURL",
		"BROKER_USERS_QUEUE":      "brokerUsersQueue",
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
