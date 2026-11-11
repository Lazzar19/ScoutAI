package store

import (
	"context"
	"fmt"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertEvaluation stores one decision.
// The same job, profile version, content hash, prompt version, and model cannot be inserted twice.
// A different profile version is a new row.
func (p *Pool) InsertEvaluation(ctx context.Context, evaluation *domain.JobEvaluation) error {
	if evaluation == nil {
		return fmt.Errorf("insert evaluation: evaluation is required")
	}
	if evaluation.EvaluatedAt.IsZero() {
		return fmt.Errorf("insert evaluation: evaluated_at is required")
	}
	ensureID(&evaluation.ID)
	_, err := p.pool.Exec(ctx, `
		INSERT INTO job_evaluations (
			id, job_id, profile_id, profile_version, content_hash, kind,
			overall_score, skills_score, role_score, experience_score, location_score, company_score,
			evaluation_data, model, prompt_version, evaluated_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13::jsonb, $14, $15, $16
		)`,
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
	)
	return mapWriteErr("insert evaluation", err)
}

// GetEvaluation reads one evaluation by id.
func (p *Pool) GetEvaluation(ctx context.Context, id string) (domain.JobEvaluation, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, profile_id, profile_version, content_hash, kind,
		       overall_score, skills_score, role_score, experience_score, location_score, company_score,
		       evaluation_data, model, prompt_version, evaluated_at
		FROM job_evaluations
		WHERE id = $1::uuid`, id)
	evaluation, err := scanEvaluation(row)
	if err != nil {
		return domain.JobEvaluation{}, mapReadErr("get evaluation", err)
	}
	return evaluation, nil
}

func scanEvaluation(row rowScanner) (domain.JobEvaluation, error) {
	var evaluation domain.JobEvaluation
	var kind string
	var data []byte
	err := row.Scan(
		&evaluation.ID,
		&evaluation.JobID,
		&evaluation.ProfileID,
		&evaluation.ProfileVersion,
		&evaluation.ContentHash,
		&kind,
		&evaluation.OverallScore,
		&evaluation.SkillsScore,
		&evaluation.RoleScore,
		&evaluation.ExperienceScore,
		&evaluation.LocationScore,
		&evaluation.CompanyScore,
		&data,
		&evaluation.Model,
		&evaluation.PromptVersion,
		&evaluation.EvaluatedAt,
	)
	if err != nil {
		return domain.JobEvaluation{}, err
	}
	evaluation.Kind = domain.DecisionKind(kind)
	evaluation.EvaluationData = data
	evaluation.EvaluatedAt = evaluation.EvaluatedAt.UTC()
	return evaluation, nil
}
