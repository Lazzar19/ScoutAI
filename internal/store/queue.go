package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const lockExpiredError = "task lock expired"

// ClaimTask marks the oldest runnable task as processing and commits that claim.
// The caller does the work after this returns, so the claim transaction stays free of other calls.
// ErrNotFound means nothing is waiting.
func (p *Pool) ClaimTask(ctx context.Context, workerID string) (domain.Task, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return domain.Task{}, fmt.Errorf("claim task: worker id is required")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("claim task: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	row := tx.QueryRow(ctx, `
		UPDATE processing_jobs
		SET status = 'processing',
		    locked_at = now(),
		    locked_by = $1,
		    updated_at = now()
		WHERE id = (
			SELECT id FROM processing_jobs
			WHERE status = 'pending' AND available_at <= now()
			ORDER BY available_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, type, job_id, source_id, content_hash, profile_version, status,
		          attempts, available_at, locked_at, locked_by, last_error`,
		workerID,
	)
	task, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, fmt.Errorf("claim task: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("claim task: commit: %w", err)
	}
	return task, nil
}

// CompleteTask finishes a task this worker has claimed.
func (p *Pool) CompleteTask(ctx context.Context, workerID, taskID string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE processing_jobs
		SET status = 'completed', updated_at = now()
		WHERE id = $1::uuid AND status = 'processing' AND locked_by = $2`,
		taskID, strings.TrimSpace(workerID),
	)
	return rowsOrNotFound("complete task", tag, err)
}

// RetryTask returns a claimed task to pending at availableAt and records the cause.
// There is no retrying status. availableAt is when a worker may claim it again.
func (p *Pool) RetryTask(ctx context.Context, workerID, taskID string, availableAt time.Time, cause error) error {
	if cause == nil {
		return fmt.Errorf("retry task: cause is required")
	}
	if availableAt.IsZero() {
		return fmt.Errorf("retry task: available_at is required")
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE processing_jobs
		SET status = 'pending',
		    attempts = attempts + 1,
		    available_at = $3,
		    locked_at = NULL,
		    locked_by = NULL,
		    last_error = $4,
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'processing' AND locked_by = $2`,
		taskID, strings.TrimSpace(workerID), availableAt, cause.Error(),
	)
	return rowsOrNotFound("retry task", tag, err)
}

// FailTask marks a claimed task failed and keeps the error for inspection.
func (p *Pool) FailTask(ctx context.Context, workerID, taskID string, cause error) error {
	if cause == nil {
		return fmt.Errorf("fail task: cause is required")
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE processing_jobs
		SET status = 'failed',
		    attempts = attempts + 1,
		    last_error = $3,
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'processing' AND locked_by = $2`,
		taskID, strings.TrimSpace(workerID), cause.Error(),
	)
	return rowsOrNotFound("fail task", tag, err)
}

// RecoverExpired requeues processing rows whose lock is older than timeout.
// A younger lock is left alone. A row that would pass maxAttempts is failed instead of requeued.
func (p *Pool) RecoverExpired(ctx context.Context, timeout time.Duration, maxAttempts int) (recovered int, failed int, err error) {
	if timeout <= 0 {
		return 0, 0, fmt.Errorf("recover expired tasks: timeout must be positive")
	}
	if maxAttempts < 1 {
		return 0, 0, fmt.Errorf("recover expired tasks: max attempts must be positive")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("recover expired tasks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	seconds := timeout.Seconds()
	failedTag, err := tx.Exec(ctx, `
		UPDATE processing_jobs
		SET status = 'failed',
		    attempts = attempts + 1,
		    last_error = $3,
		    updated_at = now()
		WHERE status = 'processing'
		  AND locked_at < now() - make_interval(secs => $1)
		  AND attempts + 1 >= $2`,
		seconds, maxAttempts, lockExpiredError,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("recover expired tasks: fail: %w", err)
	}
	recoveredTag, err := tx.Exec(ctx, `
		UPDATE processing_jobs
		SET status = 'pending',
		    attempts = attempts + 1,
		    available_at = now(),
		    locked_at = NULL,
		    locked_by = NULL,
		    last_error = $3,
		    updated_at = now()
		WHERE status = 'processing'
		  AND locked_at < now() - make_interval(secs => $1)
		  AND attempts + 1 < $2`,
		seconds, maxAttempts, lockExpiredError,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("recover expired tasks: requeue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("recover expired tasks: commit: %w", err)
	}
	return int(recoveredTag.RowsAffected()), int(failedTag.RowsAffected()), nil
}

func rowsOrNotFound(op string, tag pgconn.CommandTag, err error) error {
	if err != nil {
		return mapWriteErr(op, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	return nil
}
