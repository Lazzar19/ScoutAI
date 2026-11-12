package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/worker"
)

// PollStore is the database access for one board poll.
type PollStore interface {
	Store
	GetSource(ctx context.Context, id string) (domain.JobSource, error)
	StartSourceRun(ctx context.Context, sourceID string, startedAt time.Time) (string, error)
	FinishSourceRun(ctx context.Context, runID, status string, discovered, processed int, runErr string, finishedAt time.Time) error
	MarkSourceChecked(ctx context.Context, sourceID string, checkedAt time.Time) error
	MarkSourceSnapshot(ctx context.Context, sourceID string, at time.Time) error
}

// Handler claims poll tasks for Greenhouse and leaves match tasks for the rules milestone.
// The fetch runs in the handler, after the claim transaction has committed.
func Handler(db PollStore, boards source.Greenhouse, logger *slog.Logger) worker.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, task domain.Task) error {
		switch task.Type {
		case domain.TaskPoll:
			return runPoll(ctx, db, boards, logger, task)
		case domain.TaskMatch:
			logger.Info("match task waiting for rules", "task_id", task.ID)
			return nil
		default:
			return fmt.Errorf("unknown task type %s", task.Type)
		}
	}
}

func runPoll(ctx context.Context, db PollStore, boards source.Greenhouse, logger *slog.Logger, task domain.Task) error {
	if task.SourceID == nil || *task.SourceID == "" {
		return fmt.Errorf("poll task %s has no source", task.ID)
	}
	src, err := db.GetSource(ctx, *task.SourceID)
	if err != nil {
		return err
	}
	if !src.Enabled {
		logger.Info("skip disabled source", "source", src.Name)
		return nil
	}
	if src.Type != domain.SourceGreenhouse {
		return fmt.Errorf("source %s type %s is not polled yet", src.Name, src.Type)
	}

	started := time.Now().UTC()
	runID, err := db.StartSourceRun(ctx, src.ID, started)
	if err != nil {
		return err
	}
	snapshot, fetchErr := boards.Fetch(ctx, src.Config.BoardToken)
	if fetchErr != nil {
		_ = db.FinishSourceRun(ctx, runID, "failed", 0, 0, fetchErr.Error(), time.Now().UTC())
		_ = db.MarkSourceChecked(ctx, src.ID, time.Now().UTC())
		return fetchErr
	}
	result, err := Apply(ctx, db, src, snapshot, time.Now().UTC())
	finished := time.Now().UTC()
	if err != nil {
		_ = db.FinishSourceRun(ctx, runID, "failed", len(snapshot.Postings)+len(snapshot.ItemErrors), result.Stored, err.Error(), finished)
		_ = db.MarkSourceChecked(ctx, src.ID, finished)
		return err
	}
	note := ""
	if len(result.ItemErrors) > 0 {
		note = fmt.Sprintf("%d postings could not be read", len(result.ItemErrors))
	}
	if err := db.FinishSourceRun(ctx, runID, "succeeded", len(snapshot.Postings)+len(snapshot.ItemErrors), result.Stored, note, finished); err != nil {
		return err
	}
	if err := db.MarkSourceChecked(ctx, src.ID, finished); err != nil {
		return err
	}
	if snapshot.Complete && len(result.ItemErrors) == 0 {
		if err := db.MarkSourceSnapshot(ctx, src.ID, finished); err != nil {
			return err
		}
	}
	logger.Info("greenhouse snapshot stored", "source", src.Name, "stored", result.Stored, "closed", result.Closed)
	return nil
}
