package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// UpsertSource inserts a source or refreshes its non-secret configuration.
// A later startup does not overwrite the cursor, enabled flag, or run timestamps.
func (p *Pool) UpsertSource(ctx context.Context, source *domain.JobSource) error {
	if source == nil {
		return fmt.Errorf("upsert source: source is required")
	}
	ensureID(&source.ID)
	cfg, err := configJSON(source.Config)
	if err != nil {
		return fmt.Errorf("upsert source: %w", err)
	}
	row := p.pool.QueryRow(ctx, `
		INSERT INTO sources (id, name, type, enabled, configuration, cursor)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, '')
		ON CONFLICT (name) DO UPDATE
		SET type = EXCLUDED.type,
		    configuration = EXCLUDED.configuration
		RETURNING id, name, type, enabled, configuration, cursor, last_checked_at, last_success_at`,
		source.ID,
		source.Name,
		string(source.Type),
		source.Enabled,
		cfg,
	)
	stored, err := scanSource(row)
	if err != nil {
		return mapWriteErr("upsert source", err)
	}
	*source = stored
	return nil
}

// ListSources returns every configured source.
func (p *Pool) ListSources(ctx context.Context) ([]domain.JobSource, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, name, type, enabled, configuration, cursor, last_checked_at, last_success_at
		FROM sources
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()
	var sources []domain.JobSource
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("list sources: %w", err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return sources, nil
}

// MarkSourceChecked records that a poll was attempted. The cursor stays where it was.
func (p *Pool) MarkSourceChecked(ctx context.Context, sourceID string, checkedAt time.Time) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE sources SET last_checked_at = $2 WHERE id = $1::uuid`,
		sourceID, checkedAt.UTC(),
	)
	return rowsOrNotFound("mark source checked", tag, err)
}

// MarkSourceSnapshot records a complete snapshot. The cursor is that timestamp, not an API since value.
func (p *Pool) MarkSourceSnapshot(ctx context.Context, sourceID string, at time.Time) error {
	at = at.UTC()
	tag, err := p.pool.Exec(ctx, `
		UPDATE sources
		SET cursor = $2, last_checked_at = $3, last_success_at = $3
		WHERE id = $1::uuid`,
		sourceID, at.Format(time.RFC3339Nano), at,
	)
	return rowsOrNotFound("mark source snapshot", tag, err)
}

// FindReferenceByExternalID returns the reference for one source and external id.
func (p *Pool) FindReferenceByExternalID(ctx context.Context, sourceID, externalID string) (domain.SourceReference, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, source_id, external_id, original_url, canonical_url, discovered_at
		FROM job_source_references
		WHERE source_id = $1::uuid AND external_id = $2`,
		sourceID, externalID,
	)
	ref, err := scanReference(row)
	if err != nil {
		return domain.SourceReference{}, mapReadErr("find reference by external id", err)
	}
	return ref, nil
}

// FindReferenceByCanonicalURL returns the URL-only reference for one source.
// A reference that also has an external id is a different identity and is not returned.
func (p *Pool) FindReferenceByCanonicalURL(ctx context.Context, sourceID, canonicalURL string) (domain.SourceReference, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, source_id, external_id, original_url, canonical_url, discovered_at
		FROM job_source_references
		WHERE source_id = $1::uuid AND external_id IS NULL AND canonical_url = $2`,
		sourceID, canonicalURL,
	)
	ref, err := scanReference(row)
	if err != nil {
		return domain.SourceReference{}, mapReadErr("find reference by canonical url", err)
	}
	return ref, nil
}

// UpdateJobSnapshot writes the mutable posting fields and the reference URLs in one transaction.
func (p *Pool) UpdateJobSnapshot(ctx context.Context, job domain.Job, ref domain.SourceReference) error {
	locations, err := locationsJSON(job.Locations)
	if err != nil {
		return fmt.Errorf("update job snapshot: %w", err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("update job snapshot: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	tag, err := tx.Exec(ctx, `
		UPDATE jobs
		SET title = $2,
		    company = $3,
		    locations = $4::jsonb,
		    description = $5,
		    language = $6,
		    url = $7,
		    employment_type = $8,
		    posted_at = $9,
		    content_hash = $10,
		    completeness = $11,
		    status = $12,
		    updated_at = now()
		WHERE id = $1::uuid`,
		job.ID,
		job.Title,
		job.Company,
		locations,
		job.Description,
		job.Language,
		job.URL,
		job.EmploymentType,
		job.PostedAt,
		job.ContentHash,
		string(job.Completeness),
		string(job.Status),
	)
	if err != nil {
		return mapWriteErr("update job snapshot", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update job snapshot: %w", ErrNotFound)
	}
	tag, err = tx.Exec(ctx, `
		UPDATE job_source_references
		SET original_url = $2, canonical_url = $3
		WHERE id = $1::uuid`,
		ref.ID, ref.OriginalURL, ref.CanonicalURL,
	)
	if err != nil {
		return mapWriteErr("update job snapshot reference", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update job snapshot reference: %w", ErrNotFound)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("update job snapshot: commit: %w", err)
	}
	return nil
}

// CloseMissingJobs marks open jobs from this source closed when their external id is absent.
// The caller must only use this after a complete snapshot with no unread postings.
func (p *Pool) CloseMissingJobs(ctx context.Context, sourceID string, seenExternalIDs []string) (int, error) {
	if seenExternalIDs == nil {
		seenExternalIDs = []string{}
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE jobs
		SET status = 'closed', updated_at = now()
		WHERE status = 'open'
		  AND id IN (
		      SELECT job_id FROM job_source_references
		      WHERE source_id = $1::uuid
		        AND external_id IS NOT NULL
		        AND NOT (external_id = ANY($2::text[]))
		  )`,
		sourceID, seenExternalIDs,
	)
	if err != nil {
		return 0, fmt.Errorf("close missing jobs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// StartSourceRun opens a run row so a failed poll stays visible.
func (p *Pool) StartSourceRun(ctx context.Context, sourceID string, startedAt time.Time) (string, error) {
	id := domain.NewID()
	_, err := p.pool.Exec(ctx, `
		INSERT INTO source_runs (id, source_id, started_at, status, items_discovered, items_processed)
		VALUES ($1::uuid, $2::uuid, $3, 'running', 0, 0)`,
		id, sourceID, startedAt.UTC(),
	)
	if err != nil {
		return "", mapWriteErr("start source run", err)
	}
	return id, nil
}

// FinishSourceRun closes a run. runErr is empty when the poll stored its snapshot.
func (p *Pool) FinishSourceRun(ctx context.Context, runID, status string, discovered, processed int, runErr string, finishedAt time.Time) error {
	var failure any
	if runErr != "" {
		failure = runErr
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE source_runs
		SET finished_at = $2, status = $3, items_discovered = $4, items_processed = $5, error = $6
		WHERE id = $1::uuid`,
		runID, finishedAt.UTC(), status, discovered, processed, failure,
	)
	return rowsOrNotFound("finish source run", tag, err)
}

// EnqueueMatch inserts a match task. The same active task is already queued when this returns nil.
func (p *Pool) EnqueueMatch(ctx context.Context, jobID, contentHash string, profileVersion int) error {
	task := domain.Task{
		Type:           domain.TaskMatch,
		JobID:          &jobID,
		ContentHash:    &contentHash,
		ProfileVersion: &profileVersion,
		Status:         domain.TaskPending,
		AvailableAt:    time.Now().UTC(),
	}
	err := p.InsertTask(ctx, &task)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

// EnqueuePoll inserts one poll for a source. An active poll for that source is left as it is.
func (p *Pool) EnqueuePoll(ctx context.Context, sourceID string) error {
	task := domain.Task{
		Type:        domain.TaskPoll,
		SourceID:    &sourceID,
		Status:      domain.TaskPending,
		AvailableAt: time.Now().UTC(),
	}
	err := p.InsertTask(ctx, &task)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

// ActivePoll returns the pending or processing poll for a source.
func (p *Pool) ActivePoll(ctx context.Context, sourceID string) (domain.Task, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, type, job_id, source_id, content_hash, profile_version, status,
		       attempts, available_at, locked_at, locked_by, last_error
		FROM processing_jobs
		WHERE type = 'poll' AND source_id = $1::uuid AND status IN ('pending', 'processing')
		ORDER BY created_at
		LIMIT 1`, sourceID)
	task, err := scanTask(row)
	if err != nil {
		return domain.Task{}, mapReadErr("active poll", err)
	}
	return task, nil
}

// FindActiveMatch reports whether this job, hash, and profile version already have an active match task.
func (p *Pool) FindActiveMatch(ctx context.Context, jobID, contentHash string, profileVersion int) (domain.Task, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, type, job_id, source_id, content_hash, profile_version, status,
		       attempts, available_at, locked_at, locked_by, last_error
		FROM processing_jobs
		WHERE type = 'match'
		  AND job_id = $1::uuid
		  AND content_hash = $2
		  AND profile_version = $3
		  AND status IN ('pending', 'processing')
		LIMIT 1`,
		jobID, contentHash, profileVersion,
	)
	task, err := scanTask(row)
	if err != nil {
		return domain.Task{}, mapReadErr("find active match", err)
	}
	return task, nil
}
