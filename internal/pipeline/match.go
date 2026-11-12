package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/match"
)

func runMatch(ctx context.Context, db TaskStore, rules match.Rules, recipient string, logger *slog.Logger, task domain.Task) error {
	if task.JobID == nil || strings.TrimSpace(*task.JobID) == "" || task.ContentHash == nil || strings.TrimSpace(*task.ContentHash) == "" || task.ProfileVersion == nil {
		return fmt.Errorf("match task %s is missing the job, hash, or profile version", task.ID)
	}
	job, err := db.GetJob(ctx, *task.JobID)
	if err != nil {
		return err
	}
	if job.ContentHash != *task.ContentHash {
		logger.Info("match task is stale", "task_id", task.ID, "job_id", job.ID)
		return nil
	}
	profile, err := db.GetProfileByVersion(ctx, *task.ProfileVersion)
	if err != nil {
		return err
	}
	outcome := match.Decide(job, profile, rules)
	evaluation := domain.JobEvaluation{
		JobID:           job.ID,
		ProfileID:       profile.ID,
		ProfileVersion:  profile.Version,
		ContentHash:     job.ContentHash,
		PromptVersion:   match.RulesPrompt,
		Model:           match.RulesModel,
		Kind:            outcome.Kind,
		OverallScore:    outcome.Overall,
		SkillsScore:     outcome.Skills,
		RoleScore:       outcome.Role,
		ExperienceScore: outcome.Experience,
		LocationScore:   outcome.Location,
		CompanyScore:    outcome.Company,
		EvaluatedAt:     time.Now().UTC(),
	}
	var notification *domain.Notification
	if outcome.Notify && job.Status != domain.JobClosed {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			recipient = "candidate"
		}
		notification = &domain.Notification{
			JobID:     job.ID,
			Recipient: recipient,
			Type:      outcome.Kind,
			Channel:   domain.ChannelTelegram,
			Status:    domain.DeliveryPending,
		}
	}
	if err := db.SaveDecision(ctx, &evaluation, notification); err != nil {
		return err
	}
	logger.Info("match decision stored", "job_id", job.ID, "kind", outcome.Kind, "notify", notification != nil)
	return nil
}
