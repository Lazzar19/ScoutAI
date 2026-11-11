package store

import (
	"context"
	"fmt"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertTask stores one queue row.
// A second active match for the same job, content hash, and profile version is rejected.
// A second active poll for the same source is rejected. A completed row does not block a later insert.
func (p *Pool) InsertTask(ctx context.Context, task *domain.Task) error {
	if task == nil {
		return fmt.Errorf("insert task: task is required")
	}
	ensureID(&task.ID)
	availableAt := any(task.AvailableAt)
	if task.AvailableAt.IsZero() {
		availableAt = nil
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO processing_jobs (
			id, type, job_id, source_id, content_hash, profile_version, status,
			attempts, available_at, locked_at, locked_by, last_error
		) VALUES (
			$1::uuid, $2, $3::uuid, $4::uuid, $5, $6, $7,
			$8, COALESCE($9::timestamptz, now()), $10, $11, $12
		)`,
		task.ID,
		string(task.Type),
		task.JobID,
		task.SourceID,
		task.ContentHash,
		task.ProfileVersion,
		string(task.Status),
		task.Attempts,
		availableAt,
		task.LockedAt,
		task.LockedBy,
		task.LastError,
	)
	if err != nil {
		return mapWriteErr("insert task", err)
	}
	if task.AvailableAt.IsZero() {
		stored, err := p.GetTask(ctx, task.ID)
		if err != nil {
			return err
		}
		task.AvailableAt = stored.AvailableAt
	}
	return nil
}

// GetTask reads one queue row by id.
func (p *Pool) GetTask(ctx context.Context, id string) (domain.Task, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, type, job_id, source_id, content_hash, profile_version, status,
		       attempts, available_at, locked_at, locked_by, last_error
		FROM processing_jobs
		WHERE id = $1::uuid`, id)
	task, err := scanTask(row)
	if err != nil {
		return domain.Task{}, mapReadErr("get task", err)
	}
	return task, nil
}

func scanTask(row rowScanner) (domain.Task, error) {
	var task domain.Task
	var taskType, status string
	err := row.Scan(
		&task.ID,
		&taskType,
		&task.JobID,
		&task.SourceID,
		&task.ContentHash,
		&task.ProfileVersion,
		&status,
		&task.Attempts,
		&task.AvailableAt,
		&task.LockedAt,
		&task.LockedBy,
		&task.LastError,
	)
	if err != nil {
		return domain.Task{}, err
	}
	task.Type = domain.TaskType(taskType)
	task.Status = domain.TaskStatus(status)
	task.AvailableAt = task.AvailableAt.UTC()
	task.LockedAt = utcPtr(task.LockedAt)
	return task, nil
}
