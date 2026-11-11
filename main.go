package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/config"
	"github.com/Lazzar19/ScoutAI/internal/health"
	"github.com/Lazzar19/ScoutAI/internal/store"
	"github.com/Lazzar19/ScoutAI/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration is invalid", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("process stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	logger.Info("applying migrations")
	if err := store.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
		return err
	}

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	var wg sync.WaitGroup
	logger.Info("starting queue workers", "count", cfg.WorkerCount)
	for i := range cfg.WorkerCount {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := worker.ID(i)
			err := worker.Worker{
				Queue:       pool,
				Handler:     worker.FakeHandler(logger),
				ID:          id,
				TaskTimeout: cfg.TaskTimeout,
				MaxAttempts: cfg.TaskMaxAttempts,
				Poll:        cfg.WorkerPoll,
				RetryBase:   cfg.TaskRetryBase,
				Logger:      logger,
			}.Run(workerCtx)
			if err != nil {
				logger.Error("worker stopped", "worker", id, "err", err)
			}
		}(i)
	}
	defer func() {
		cancelWorkers()
		wg.Wait()
	}()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           health.Handler(pool, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
