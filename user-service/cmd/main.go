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
	"github.com/aleksandarv/file-uploader/common/tls"
	"github.com/aleksandarv/file-uploader/user-service/internal/api"
	sqlc "github.com/aleksandarv/file-uploader/user-service/internal/repository/gen"
	usersrepo "github.com/aleksandarv/file-uploader/user-service/internal/repository/users"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/jackc/pgx/v5/pgxpool"
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
		debug         bool
		healthPort    int
		grpcPort      int
		tlsCertFile   string
		tlsKeyFile    string
		tlsCACertFile string
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&healthPort, "healthPort", 8080, "HTTP port for the health endpoint")
	flag.IntVar(&grpcPort, "grpcPort", 8443, "mTLS port for the internal gRPC API")
	flag.StringVar(&tlsCertFile, "tlsCertFile", "", "Path to this service's TLS certificate")
	flag.StringVar(&tlsKeyFile, "tlsKeyFile", "", "Path to this service's TLS private key")
	flag.StringVar(&tlsCACertFile, "tlsCACertFile", "", "Path to the CA certificate used to verify client certificates")
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

	pool, err := pgxpool.New(ctx, dbURL)
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
	usersHandler := api.NewUsersHandler(userssvc.New(repo))

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

	grpcAddr := ":" + strconv.Itoa(grpcPort)
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("failed to listen for gRPC", "addr", grpcAddr, "err", err)
		return err
	}
	grpcSrv := api.GRPCServer(log, usersHandler, grpc.Creds(credentials.NewTLS(tlsCfg)))

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
		log.Info("Start mTLS gRPC server on", "host", grpcAddr)
		errc <- grpcSrv.Serve(grpcLis)
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
		"DEBUG":            "debug",
		"HEALTH_PORT":      "healthPort",
		"GRPC_PORT":        "grpcPort",
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
