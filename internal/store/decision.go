package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// SaveDecision stores an evaluation and, when notify is set, its pending outbox row.
// The same cache key reuses the evaluation. The same notification type is not inserted twice.
func (p *Pool) SaveDecision(ctx context.Context, evaluation *domain.JobEvaluation, notify *domain.Notification) error {
	if evaluation == nil {
		return fmt.Errorf("save decision: evaluation is required")
	}
	if evaluation.EvaluatedAt.IsZero() {
		return fmt.Errorf("save decision: evaluated_at is required")
	}
	ensureID(&evaluation.ID)

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save decision: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO job_evaluations (
			id, job_id, profile_id, profile_version, content_hash, kind,
			overall_score, skills_score, role_score, experience_score, location_score, company_score,
			evaluation_data, model, prompt_version, evaluated_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13::jsonb, $14, $15, $16
		)
		ON CONFLICT ON CONSTRAINT job_evaluations_cache_key DO NOTHING
		RETURNING id`,
		evaluation.ID,
		evaluation.JobID,
		evaluation.ProfileID,
		evaluation.ProfileVersion,
		evaluation.ContentHash,
		string(evaluation.Kind),
		evaluation.OverallScore,
		evaluation.SkillsScore,
		evaluation.RoleScore,
		evaluation.ExperienceScore,
		evaluation.LocationScore,
		evaluation.CompanyScore,
		jsonbArg(evaluation.EvaluationData),
		evaluation.Model,
		evaluation.PromptVersion,
		evaluation.EvaluatedAt,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT id
			FROM job_evaluations
			WHERE job_id = $1::uuid
			  AND profile_version = $2
			  AND content_hash = $3
			  AND prompt_version = $4
			  AND model = $5`,
			evaluation.JobID,
			evaluation.ProfileVersion,
			evaluation.ContentHash,
			evaluation.PromptVersion,
			evaluation.Model,
		).Scan(&id)
	}
	if err != nil {
		return mapWriteErr("save decision", err)
	}
	evaluation.ID = id

	if notify != nil {
		ensureID(&notify.ID)
		notify.JobID = evaluation.JobID
		notify.EvaluationID = &evaluation.ID
		if notify.Channel == "" {
			notify.Channel = domain.ChannelTelegram
		}
		if notify.Status == "" {
			notify.Status = domain.DeliveryPending
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (
				id, job_id, evaluation_id, recipient, notification_type, channel, status,
				external_message_id, sent_at, error
			) VALUES (
				$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7,
				$8, $9, $10
			)
			ON CONFLICT ON CONSTRAINT notifications_job_recipient_type_key DO NOTHING`,
			notify.ID,
			notify.JobID,
			notify.EvaluationID,
			notify.Recipient,
			string(notify.Type),
			string(notify.Channel),
			string(notify.Status),
			notify.ExternalMessageID,
			notify.SentAt,
			notify.Error,
		); err != nil {
			return mapWriteErr("save decision notification", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save decision: commit: %w", err)
	}
	return nil
}

// FindEvaluationByKey reads the cached decision for one job and profile version.
func (p *Pool) FindEvaluationByKey(ctx context.Context, jobID string, profileVersion int, contentHash, promptVersion, model string) (domain.JobEvaluation, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, profile_id, profile_version, content_hash, kind,
		       overall_score, skills_score, role_score, experience_score, location_score, company_score,
		       evaluation_data, model, prompt_version, evaluated_at
		FROM job_evaluations
		WHERE job_id = $1::uuid
		  AND profile_version = $2
		  AND content_hash = $3
		  AND prompt_version = $4
		  AND model = $5`,
		jobID, profileVersion, contentHash, promptVersion, model,
	)
	evaluation, err := scanEvaluation(row)
	if err != nil {
		return domain.JobEvaluation{}, mapReadErr("find evaluation", err)
	}
	return evaluation, nil
}
