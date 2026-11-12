package schedule

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func TestDueWaitsForTheInterval(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if !Due(nil, now, time.Hour, 0) {
		t.Fatal("a source with no check was not due")
	}
	checked := now.Add(-30 * time.Minute)
	if Due(&checked, now, time.Hour, 0) {
		t.Fatal("a recent check was due")
	}
	checked = now.Add(-time.Hour)
	if !Due(&checked, now, time.Hour, 0) {
		t.Fatal("a check one interval ago was not due")
	}
}

func TestEnqueueDuePollsGreenhouseOnce(t *testing.T) {
	pool := openSchedulePool(t)
	ctx := context.Background()
	src := domain.JobSource{
		Name:    "greenhouse-" + domain.NewID(),
		Type:    domain.SourceGreenhouse,
		Enabled: true,
		Config:  domain.SourceConfig{BoardToken: "example"},
	}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	sched := Scheduler{
		Store:    pool,
		Interval: time.Hour,
		Now:      func() time.Time { return now },
		Logger:   slog.New(slog.DiscardHandler),
	}
	if err := sched.EnqueueDue(ctx); err != nil {
		t.Fatalf("EnqueueDue() error = %v", err)
	}
	if _, err := pool.ActivePoll(ctx, src.ID); err != nil {
		t.Fatalf("ActivePoll() error = %v", err)
	}
	if err := sched.EnqueueDue(ctx); err != nil {
		t.Fatalf("second EnqueueDue() error = %v", err)
	}

	poll, err := pool.ActivePoll(ctx, src.ID)
	if err != nil {
		t.Fatalf("ActivePoll() error = %v", err)
	}
	finishPoll(t, ctx, pool, poll.ID)
	if err := pool.MarkSourceChecked(ctx, src.ID, now); err != nil {
		t.Fatalf("MarkSourceChecked() error = %v", err)
	}
	if err := sched.EnqueueDue(ctx); err != nil {
		t.Fatalf("EnqueueDue() after check error = %v", err)
	}
	if _, err := pool.ActivePoll(ctx, src.ID); err == nil {
		t.Fatal("a fresh check was polled again")
	}

	if err := pool.MarkSourceChecked(ctx, src.ID, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("MarkSourceChecked() error = %v", err)
	}
	if err := sched.EnqueueDue(ctx); err != nil {
		t.Fatalf("EnqueueDue() after interval error = %v", err)
	}
	if _, err := pool.ActivePoll(ctx, src.ID); err != nil {
		t.Fatalf("ActivePoll() after interval error = %v", err)
	}
}

func finishPoll(t *testing.T, ctx context.Context, pool *store.Pool, pollID string) {
	t.Helper()
	for {
		task, err := pool.ClaimTask(ctx, "schedule-test")
		if err != nil {
			t.Fatalf("ClaimTask() error = %v", err)
		}
		if err := pool.CompleteTask(ctx, "schedule-test", task.ID); err != nil {
			t.Fatalf("CompleteTask() error = %v", err)
		}
		if task.ID == pollID {
			return
		}
	}
}

func openSchedulePool(t *testing.T) *store.Pool {
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
