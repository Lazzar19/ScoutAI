package store

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func TestPendingTaskSurvivesReopen(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := testContext(t)
	if err := Migrate(ctx, databaseURL, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	source := insertSource(t, ctx, pool, "poll-"+domain.NewID())
	task := newPoll(source.ID, domain.TaskPending, time.Now().Add(-time.Minute))
	if err := pool.InsertTask(ctx, &task); err != nil {
		t.Fatalf("InsertTask() error = %v", err)
	}
	pool.Close()

	reopened, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(reopened.Close)
	got, err := reopened.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
}

func TestClaimTakesTheOldestPendingTask(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	later := insertPoll(t, ctx, pool, time.Now().Add(time.Hour))
	sooner := insertPoll(t, ctx, pool, time.Now().Add(-time.Hour))

	got, err := pool.ClaimTask(ctx, "worker-a")
	if err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if got.ID != sooner.ID || got.Status != domain.TaskProcessing || got.LockedBy == nil || *got.LockedBy != "worker-a" {
		t.Fatalf("claimed = %+v, want %s", got, sooner.ID)
	}
	if _, err := pool.ClaimTask(ctx, "worker-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second claim error = %v, want ErrNotFound while the other task waits", err)
	}
	waiting, err := pool.GetTask(ctx, later.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if waiting.Status != domain.TaskPending {
		t.Fatalf("later task status = %s, want pending", waiting.Status)
	}
}

func TestTwoWorkersDoNotClaimTheSameTask(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	first := insertPoll(t, ctx, pool, time.Now().Add(-time.Minute))
	second := insertPoll(t, ctx, pool, time.Now().Add(-time.Minute))

	start := make(chan struct{})
	var wg sync.WaitGroup
	claimed := make(chan domain.Task, 2)
	errs := make(chan error, 2)
	for _, workerID := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			<-start
			task, err := pool.ClaimTask(ctx, workerID)
			if err != nil {
				errs <- err
				return
			}
			claimed <- task
		}(workerID)
	}
	close(start)
	wg.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	seen := map[string]bool{}
	for task := range claimed {
		if seen[task.ID] {
			t.Fatalf("task %s was claimed twice", task.ID)
		}
		seen[task.ID] = true
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("claimed ids = %v, want %s and %s", seen, first.ID, second.ID)
	}
}

func TestYoungLockIsNotStolen(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	task := insertProcessingPoll(t, ctx, pool, time.Now(), "holder")

	recovered, failed, err := pool.RecoverExpired(ctx, time.Minute, 5)
	if err != nil {
		t.Fatalf("RecoverExpired() error = %v", err)
	}
	if recovered != 0 || failed != 0 {
		t.Fatalf("recovered = %d failed = %d, want 0 and 0", recovered, failed)
	}
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskProcessing || got.LockedBy == nil || *got.LockedBy != "holder" {
		t.Fatalf("task = %+v, want the young lock kept", got)
	}
	if _, err := pool.ClaimTask(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimTask() error = %v, want ErrNotFound", err)
	}
}

func TestExpiredLockIsRecovered(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	task := insertProcessingPoll(t, ctx, pool, time.Now().Add(-2*time.Minute), "holder")

	recovered, failed, err := pool.RecoverExpired(ctx, time.Minute, 5)
	if err != nil {
		t.Fatalf("RecoverExpired() error = %v", err)
	}
	if recovered != 1 || failed != 0 {
		t.Fatalf("recovered = %d failed = %d, want 1 and 0", recovered, failed)
	}
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskPending || got.LockedBy != nil || got.Attempts != 1 {
		t.Fatalf("task = %+v, want pending with one attempt and no lock", got)
	}
	claimed, err := pool.ClaimTask(ctx, "worker-b")
	if err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if claimed.ID != task.ID {
		t.Fatalf("claimed %s, want %s", claimed.ID, task.ID)
	}
}

func TestExpiredLockAtAttemptCapIsFailed(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	task := insertProcessingPoll(t, ctx, pool, time.Now().Add(-2*time.Minute), "holder")
	if _, err := pool.pool.Exec(ctx, `UPDATE processing_jobs SET attempts = 4 WHERE id = $1::uuid`, task.ID); err != nil {
		t.Fatalf("set attempts: %v", err)
	}

	recovered, failed, err := pool.RecoverExpired(ctx, time.Minute, 5)
	if err != nil {
		t.Fatalf("RecoverExpired() error = %v", err)
	}
	if recovered != 0 || failed != 1 {
		t.Fatalf("recovered = %d failed = %d, want 0 and 1", recovered, failed)
	}
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskFailed || got.Attempts != 5 || got.LastError == nil || *got.LastError != lockExpiredError {
		t.Fatalf("task = %+v, want a readable failed row", got)
	}
}

func TestRetryWaitsUntilAvailableAt(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	task := insertPoll(t, ctx, pool, time.Now().Add(-time.Minute))
	if _, err := pool.ClaimTask(ctx, "worker-a"); err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	availableAt := time.Now().Add(time.Hour).UTC()
	if err := pool.RetryTask(ctx, "worker-a", task.ID, availableAt, errors.New("source timed out")); err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskPending || got.Attempts != 1 || got.LockedBy != nil {
		t.Fatalf("task = %+v, want pending", got)
	}
	if got.LastError == nil || *got.LastError != "source timed out" {
		t.Fatalf("last_error = %v", got.LastError)
	}
	other := insertPoll(t, ctx, pool, time.Now().Add(-time.Minute))
	claimed, err := pool.ClaimTask(ctx, "worker-b")
	if err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if claimed.ID != other.ID {
		t.Fatalf("claimed %s, want the task that is already available", other.ID)
	}
}

func TestFailedTaskStaysReadable(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	task := insertPoll(t, ctx, pool, time.Now().Add(-time.Minute))
	if _, err := pool.ClaimTask(ctx, "worker-a"); err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if err := pool.FailTask(ctx, "worker-a", task.ID, errors.New("attempts exhausted")); err != nil {
		t.Fatalf("FailTask() error = %v", err)
	}
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskFailed || got.Attempts != 1 || got.LastError == nil || *got.LastError != "attempts exhausted" {
		t.Fatalf("task = %+v, want a readable failure", got)
	}
	if _, err := pool.ClaimTask(ctx, "worker-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimTask() error = %v, want ErrNotFound", err)
	}
}

func TestCompleteTaskLeavesTheQueue(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	drainPending(t, ctx, pool)
	task := insertPoll(t, ctx, pool, time.Now().Add(-time.Minute))
	if _, err := pool.ClaimTask(ctx, "worker-a"); err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if err := pool.CompleteTask(ctx, "worker-a", task.ID); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	got, err := pool.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != domain.TaskCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if _, err := pool.ClaimTask(ctx, "worker-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimTask() error = %v, want ErrNotFound", err)
	}
}

func drainPending(t *testing.T, ctx context.Context, pool *Pool) {
	t.Helper()
	for {
		task, err := pool.ClaimTask(ctx, "drain")
		if errors.Is(err, ErrNotFound) {
			return
		}
		if err != nil {
			t.Fatalf("drain claim error = %v", err)
		}
		if err := pool.CompleteTask(ctx, "drain", task.ID); err != nil {
			t.Fatalf("drain complete error = %v", err)
		}
	}
}

func insertPoll(t *testing.T, ctx context.Context, pool *Pool, availableAt time.Time) domain.Task {
	t.Helper()
	source := insertSource(t, ctx, pool, "poll-"+domain.NewID())
	task := newPoll(source.ID, domain.TaskPending, availableAt)
	if err := pool.InsertTask(ctx, &task); err != nil {
		t.Fatalf("InsertTask() error = %v", err)
	}
	return task
}

func insertProcessingPoll(t *testing.T, ctx context.Context, pool *Pool, lockedAt time.Time, holder string) domain.Task {
	t.Helper()
	source := insertSource(t, ctx, pool, "poll-"+domain.NewID())
	task := newPoll(source.ID, domain.TaskProcessing, time.Now().Add(-time.Hour))
	task.LockedAt = &lockedAt
	task.LockedBy = &holder
	if err := pool.InsertTask(ctx, &task); err != nil {
		t.Fatalf("InsertTask() error = %v", err)
	}
	return task
}

func newPoll(sourceID string, status domain.TaskStatus, availableAt time.Time) domain.Task {
	return domain.Task{
		Type:        domain.TaskPoll,
		SourceID:    &sourceID,
		Status:      status,
		AvailableAt: availableAt.UTC(),
	}
}
