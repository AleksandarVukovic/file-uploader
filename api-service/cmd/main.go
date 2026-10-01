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

	"github.com/aleksandarv/file-uploader/api-service/internal/api"
	"github.com/aleksandarv/file-uploader/api-service/internal/fileservice"
	"github.com/aleksandarv/file-uploader/common/http/client"
	"github.com/aleksandarv/file-uploader/common/logger"
)

func main() {
	var (
		debug          bool
		port           int
		fileServiceURL string
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	flag.IntVar(&port, "port", 8080, "HTTP port")
	flag.StringVar(&fileServiceURL, "fileServiceURL", "http://localhost:8081", "URL of the file-service")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	fsURL, err := url.Parse(fileServiceURL)
	if err != nil {
		log.Error("invalid fileServiceURL", "error", err)
		os.Exit(1)
	}

	fsClient := fileservice.NewClient(fsURL.Scheme, fsURL.Host, debug, client.NewDoer(debug))
	filesService := api.NewFilesSvc(fsClient)

	handler := api.Routes(log, filesService, api.NewHealthSvc())

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
		"DEBUG":            "debug",
		"PORT":             "port",
		"FILE_SERVICE_URL": "fileServiceURL",
	}
	for env, flagName := range envToFlag {
		if val := os.Getenv(env); val != "" {
			os.Args = append(os.Args, fmt.Sprintf("--%s=%s", flagName, val))
		}
	}
}
