package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/aleksandarv/golang-starter/internal/logger"
)

func main() {
	var (
		debug bool
	)
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose logging")
	loadFlagsFromEnv()
	flag.Parse()

	log := logger.NewLogger(debug)
	ctx := logger.WithCtx(context.Background(), log)
	log.Info("starting application")

	errc := make(chan error)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		<-ctx.Done()
		log.Info("application shutdown requested, shutting down gracefully")

		// TODO: uncomment following lines
		// do shutdown with 30s timeout
		// ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		// defer cancel()

		// TODO: add shutdown logic below
		log.Info("application shutdown completed")
	})

	// waiting on some signal to shutdown the application
	err := <-errc
	log.Info("application exiting", "reason", err)

	// trigger shutdown goroutine process
	cancel()
	wg.Wait()
}

func loadFlagsFromEnv() {
	envToFlag := map[string]string{
		"DEBUG": "debug",
	}
	for env, flagName := range envToFlag {
		if val := os.Getenv(env); val != "" {
			os.Args = append(os.Args, fmt.Sprintf("--%s=%s", flagName, val))
		}
	}
}
