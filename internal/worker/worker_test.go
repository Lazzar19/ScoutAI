package worker

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func TestBackoffStaysWithinAQuarterOfTheDelay(t *testing.T) {
	base := 100 * time.Millisecond
	delay := backoff(1, base)
	if delay < base || delay >= base+base/4 {
		t.Fatalf("backoff(1) = %s, want [%s, %s)", delay, base, base+base/4)
	}
	capped := backoff(8, base)
	limit := base << 4
	if capped < limit || capped >= limit+limit/4 {
		t.Fatalf("backoff(8) = %s, want the shift to stay capped at %s", capped, limit)
	}
}

func TestWorkerCompletesAFakeTask(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	task := insertPoll(t, ctx, pool)

	stop := startWorker(t, testWorker(pool, FakeHandler(slog.New(slog.DiscardHandler)), 5))
	defer stop()

	waitForStatus(t, ctx, pool, task.ID, domain.TaskCompleted)
}

func TestWorkerFailsATaskPastTheAttemptCap(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	task := insertPoll(t, ctx, pool)

	w := testWorker(pool, func(context.Context, domain.Task) error {
		return context.DeadlineExceeded
	}, 2)
	w.RetryBase = 10 * time.Millisecond
	stop := startWorker(t, w)
	defer stop()

	waitForStatus(t, ctx, pool, task.ID, domain.TaskFailed)
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.LastError == nil || got.Attempts != 2 {
		t.Fatalf("task = %+v, want the failure kept after two attempts", got)
	}
}

func TestTwoWorkersDoNotRunTheSameFakeTask(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	first := insertPoll(t, ctx, pool)
	second := insertPoll(t, ctx, pool)

	var mu sync.Mutex
	calls := map[string]int{}
	handler := func(_ context.Context, task domain.Task) error {
		mu.Lock()
		calls[task.ID]++
		mu.Unlock()
		return nil
	}

	var stops []func()
	for i := range 2 {
		w := testWorker(pool, handler, 5)
		w.ID = ID(i)
		stops = append(stops, startWorker(t, w))
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()

	waitForStatus(t, ctx, pool, first.ID, domain.TaskCompleted)
	waitForStatus(t, ctx, pool, second.ID, domain.TaskCompleted)

	mu.Lock()
	defer mu.Unlock()
	if calls[first.ID] != 1 || calls[second.ID] != 1 {
		t.Fatalf("handler calls = %v, want each task once", calls)
	}
}

func TestWorkerStopsWhenCancelled(t *testing.T) {
	pool := openPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = testWorker(pool, FakeHandler(nil), 5).Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func startWorker(t *testing.T, w Worker) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("worker %s did not stop", w.ID)
		}
	}
}

func testWorker(pool *store.Pool, handler Handler, maxAttempts int) Worker {
	return Worker{
		Queue:       pool,
		Handler:     handler,
		ID:          "worker-test",
		TaskTimeout: time.Minute,
		MaxAttempts: maxAttempts,
		Poll:        15 * time.Millisecond,
		RetryBase:   15 * time.Millisecond,
		Logger:      slog.New(slog.DiscardHandler),
	}
}

func waitForStatus(t *testing.T, ctx context.Context, pool *store.Pool, id string, want domain.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last domain.TaskStatus
	for time.Now().Before(deadline) {
		got, err := pool.GetTask(ctx, id)
		if err == nil {
			last = got.Status
			if got.Status == want {
				return
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("task %s status = %s, want %s", id, last, want)
}

func openPool(t *testing.T) *store.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.Migrate(ctx, databaseURL, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertPoll(t *testing.T, ctx context.Context, pool *store.Pool) domain.Task {
	t.Helper()
	source := domain.JobSource{
		Name:    "poll-" + domain.NewID(),
		Type:    domain.SourceGreenhouse,
		Enabled: true,
		Config:  domain.SourceConfig{BoardToken: "example"},
	}
	if err := pool.InsertSource(ctx, &source); err != nil {
		t.Fatalf("InsertSource() error = %v", err)
	}
	task := domain.Task{
		Type:        domain.TaskPoll,
		SourceID:    &source.ID,
		Status:      domain.TaskPending,
		AvailableAt: time.Now().Add(-time.Minute).UTC(),
	}
	if err := pool.InsertTask(ctx, &task); err != nil {
		t.Fatalf("InsertTask() error = %v", err)
	}
	return task
}
