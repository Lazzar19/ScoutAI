package store

import (
	"context"
	"fmt"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertJobWithReference stores a job and the source row that identifies it in one transaction.
// A conflict on the reference rolls the job back, so a rejected duplicate leaves no job behind.
func (p *Pool) InsertJobWithReference(ctx context.Context, job *domain.Job, ref *domain.SourceReference) error {
	if job == nil || ref == nil {
		return fmt.Errorf("insert job with reference: job and reference are required")
	}
	if job.DiscoveredAt.IsZero() || ref.DiscoveredAt.IsZero() {
		return fmt.Errorf("insert job with reference: discovered_at is required")
	}
	ensureID(&job.ID)
	ensureID(&ref.ID)
	if ref.JobID != "" && ref.JobID != job.ID {
		return fmt.Errorf("insert job with reference: reference job_id does not match job id")
	}
	ref.JobID = job.ID
	ref.ExternalID = nilIfBlank(ref.ExternalID)

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("insert job with reference: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := insertJob(ctx, tx, job); err != nil {
		return err
	}
	if err := insertReference(ctx, tx, ref); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("insert job with reference: commit: %w", err)
	}
	return nil
}

func insertJob(ctx context.Context, q querier, job *domain.Job) error {
	locations, err := locationsJSON(job.Locations)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	_, err = q.Exec(ctx, `
		INSERT INTO jobs (
			id, kind, completeness, title, company, author, locations, description,
			language, url, apply_method, employment_type, experience_level, deadline,
			posted_at, discovered_at, content_hash, status
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8,
			$9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18
		)`,
		job.ID,
		string(job.Kind),
		string(job.Completeness),
		job.Title,
		job.Company,
		job.Author,
		locations,
		job.Description,
		job.Language,
		job.URL,
		string(job.ApplyMethod),
		job.EmploymentType,
		job.Experience,
		job.Deadline,
		job.PostedAt,
		job.DiscoveredAt,
		job.ContentHash,
		string(job.Status),
	)
	return mapWriteErr("insert job", err)
}

func insertReference(ctx context.Context, q querier, ref *domain.SourceReference) error {
	_, err := q.Exec(ctx, `
		INSERT INTO job_source_references (
			id, job_id, source_id, external_id, original_url, canonical_url, discovered_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7
		)`,
		ref.ID,
		ref.JobID,
		ref.SourceID,
		ref.ExternalID,
		ref.OriginalURL,
		ref.CanonicalURL,
		ref.DiscoveredAt,
	)
	return mapWriteErr("insert source reference", err)
}

// GetJob reads one job by id.
func (p *Pool) GetJob(ctx context.Context, id string) (domain.Job, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, kind, completeness, title, company, author, locations, description,
		       language, url, apply_method, employment_type, experience_level, deadline,
		       posted_at, discovered_at, content_hash, status
		FROM jobs
		WHERE id = $1::uuid`, id)
	job, err := scanJob(row)
	if err != nil {
		return domain.Job{}, mapReadErr("get job", err)
	}
	return job, nil
}

// GetSourceReference reads one source reference by id.
func (p *Pool) GetSourceReference(ctx context.Context, id string) (domain.SourceReference, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, source_id, external_id, original_url, canonical_url, discovered_at
		FROM job_source_references
		WHERE id = $1::uuid`, id)
	ref, err := scanReference(row)
	if err != nil {
		return domain.SourceReference{}, mapReadErr("get source reference", err)
	}
	return ref, nil
}

func scanJob(row rowScanner) (domain.Job, error) {
	var job domain.Job
	var kind, completeness, applyMethod, status string
	var locations []byte
	err := row.Scan(
		&job.ID,
		&kind,
		&completeness,
		&job.Title,
		&job.Company,
		&job.Author,
		&locations,
		&job.Description,
		&job.Language,
		&job.URL,
		&applyMethod,
		&job.EmploymentType,
		&job.Experience,
		&job.Deadline,
		&job.PostedAt,
		&job.DiscoveredAt,
		&job.ContentHash,
		&status,
	)
	if err != nil {
		return domain.Job{}, err
	}
	job.Kind = domain.OpportunityKind(kind)
	job.Completeness = domain.Completeness(completeness)
	job.ApplyMethod = domain.ApplyMethod(applyMethod)
	job.Status = domain.JobStatus(status)
	job.Locations, err = decodeLocations(locations)
	if err != nil {
		return domain.Job{}, err
	}
	job.Deadline = utcPtr(job.Deadline)
	job.PostedAt = utcPtr(job.PostedAt)
	job.DiscoveredAt = job.DiscoveredAt.UTC()
	return job, nil
}

func scanReference(row rowScanner) (domain.SourceReference, error) {
	var ref domain.SourceReference
	err := row.Scan(
		&ref.ID,
		&ref.JobID,
		&ref.SourceID,
		&ref.ExternalID,
		&ref.OriginalURL,
		&ref.CanonicalURL,
		&ref.DiscoveredAt,
	)
	if err != nil {
		return domain.SourceReference{}, err
	}
	ref.DiscoveredAt = ref.DiscoveredAt.UTC()
	return ref, nil
}
