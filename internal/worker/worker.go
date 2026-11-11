// Package worker claims queue tasks in the same process that will later poll sources.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

// Handler runs one claimed task. It is called only after the claim transaction has committed.
type Handler func(ctx context.Context, task domain.Task) error

// Queue is the durable task state. The worker does not keep a second copy of pending work.
type Queue interface {
	ClaimTask(ctx context.Context, workerID string) (domain.Task, error)
	CompleteTask(ctx context.Context, workerID, taskID string) error
	RetryTask(ctx context.Context, workerID, taskID string, availableAt time.Time, cause error) error
	FailTask(ctx context.Context, workerID, taskID string, cause error) error
	RecoverExpired(ctx context.Context, timeout time.Duration, maxAttempts int) (recovered int, failed int, err error)
}

// Worker claims one task at a time. Several workers may run in this process.
type Worker struct {
	Queue       Queue
	Handler     Handler
	ID          string
	TaskTimeout time.Duration
	MaxAttempts int
	Poll        time.Duration
	RetryBase   time.Duration
	Logger      *slog.Logger
}

// ID identifies one worker so a claim records who holds the lock.
func ID(index int) string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "scout"
	}
	return fmt.Sprintf("%s-%d-%d", host, os.Getpid(), index)
}

// FakeHandler stands in for source polling and matching.
// It succeeds without calling another service, which keeps the claim transaction free of that work.
func FakeHandler(logger *slog.Logger) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, task domain.Task) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		logger.Info("fake task completed", "task_id", task.ID, "type", task.Type)
		return nil
	}
}

// Run recovers expired locks, then claims and finishes tasks until ctx is cancelled.
// Cancellation stops new claims. An in-flight task is retried or finished before Run returns.
func (w Worker) Run(ctx context.Context) error {
	if w.Logger == nil {
		w.Logger = slog.New(slog.DiscardHandler)
	}
	if err := w.validate(); err != nil {
		return err
	}
	w.Logger.Info("queue worker started", "worker", w.ID)
	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, _, err := w.Queue.RecoverExpired(ctx, w.TaskTimeout, w.MaxAttempts); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Logger.Error("recover expired tasks", "worker", w.ID, "err", err)
			if !w.wait(ctx) {
				return nil
			}
			continue
		}

		task, err := w.Queue.ClaimTask(ctx, w.ID)
		if errors.Is(err, store.ErrNotFound) {
			if !w.wait(ctx) {
				return nil
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Logger.Error("claim task", "worker", w.ID, "err", err)
			if !w.wait(ctx) {
				return nil
			}
			continue
		}
		w.finish(ctx, task)
	}
}

func (w Worker) finish(ctx context.Context, task domain.Task) {
	handlerCtx, cancel := context.WithTimeout(ctx, w.TaskTimeout)
	err := w.Handler(handlerCtx, task)
	cancel()

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	if err != nil {
		w.Logger.Warn("task failed", "worker", w.ID, "task_id", task.ID, "attempts", task.Attempts, "err", err)
		if task.Attempts+1 >= w.MaxAttempts {
			if failErr := w.Queue.FailTask(writeCtx, w.ID, task.ID, err); failErr != nil {
				w.Logger.Error("fail task", "worker", w.ID, "task_id", task.ID, "err", failErr)
			}
			return
		}
		delay := backoff(task.Attempts+1, w.RetryBase)
		if retryErr := w.Queue.RetryTask(writeCtx, w.ID, task.ID, time.Now().Add(delay), err); retryErr != nil {
			w.Logger.Error("retry task", "worker", w.ID, "task_id", task.ID, "err", retryErr)
		}
		return
	}
	if err := w.Queue.CompleteTask(writeCtx, w.ID, task.ID); err != nil {
		w.Logger.Error("complete task", "worker", w.ID, "task_id", task.ID, "err", err)
	}
}

func (w Worker) wait(ctx context.Context) bool {
	timer := time.NewTimer(w.Poll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (w Worker) validate() error {
	if w.Queue == nil || w.Handler == nil {
		return fmt.Errorf("worker: queue and handler are required")
	}
	if strings.TrimSpace(w.ID) == "" {
		return fmt.Errorf("worker: id is required")
	}
	if w.TaskTimeout <= 0 || w.Poll <= 0 || w.RetryBase <= 0 || w.MaxAttempts < 1 {
		return fmt.Errorf("worker: timeout, poll, retry base, and max attempts must be positive")
	}
	return nil
}

// backoff grows the wait by powers of two and adds up to 25 percent jitter.
// The shift is capped so a long failure streak cannot overflow the delay.
func backoff(attempt int, base time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	delay := base << shift
	spread := delay / 4
	if spread <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int64N(int64(spread)))
}
