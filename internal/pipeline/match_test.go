package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/match"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func TestMatchTaskStoresAPreviewWithoutAScore(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	profile := studentProfile()
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	job := insertMatchJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessPreview,
		Title:        "Systems Software Intern",
		Status:       domain.JobOpen,
	})
	handle := matchHandler(pool, match.DefaultRules())
	task := matchTask(job, profile.Version)

	if err := handle(ctx, task); err != nil {
		t.Fatalf("match error = %v", err)
	}
	if err := handle(ctx, task); err != nil {
		t.Fatalf("second match error = %v", err)
	}
	stored := requireEvaluation(t, ctx, pool, job, profile.Version)
	if stored.Kind != domain.DecisionPreview || stored.OverallScore != nil || stored.RoleScore != nil || stored.Model != match.RulesModel {
		t.Fatalf("evaluation = %+v", stored)
	}
	note, err := pool.FindNotification(ctx, job.ID, "candidate", domain.DecisionPreview)
	if err != nil {
		t.Fatalf("FindNotification() error = %v", err)
	}
	if note.Status != domain.DeliveryPending || note.EvaluationID == nil || *note.EvaluationID != stored.ID {
		t.Fatalf("notification = %+v", note)
	}
}

func TestMatchTaskStoresTheScoreAndSkipsWeakOrClosedJobs(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	profile := studentProfile()
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	company := "Example"
	strong := insertMatchJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Intern",
		Company:      &company,
		Locations:    []domain.Location{{Raw: "Remote", Remote: true}},
		Description:  "Build services in Go.",
		Status:       domain.JobOpen,
	})
	weak := insertMatchJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Engineer",
		Description:  "Students will not be considered.",
		Status:       domain.JobOpen,
	})
	closedCompany := "Example"
	closed := insertMatchJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Intern",
		Company:      &closedCompany,
		Locations:    []domain.Location{{Raw: "Remote", Remote: true}},
		Description:  "Build services in Go.",
		Status:       domain.JobClosed,
	})
	handle := matchHandler(pool, match.DefaultRules())

	if err := handle(ctx, matchTask(strong, profile.Version)); err != nil {
		t.Fatalf("strong match error = %v", err)
	}
	scored := requireEvaluation(t, ctx, pool, strong, profile.Version)
	if scored.Kind != domain.DecisionEvaluated || scored.OverallScore == nil || *scored.OverallScore != 85 {
		t.Fatalf("evaluation = %+v", scored)
	}
	if _, err := pool.FindNotification(ctx, strong.ID, "candidate", domain.DecisionEvaluated); err != nil {
		t.Fatalf("FindNotification() error = %v", err)
	}

	if err := handle(ctx, matchTask(weak, profile.Version)); err != nil {
		t.Fatalf("weak match error = %v", err)
	}
	low := requireEvaluation(t, ctx, pool, weak, profile.Version)
	if low.ExperienceScore == nil || *low.ExperienceScore != 0 || low.OverallScore == nil {
		t.Fatalf("weak evaluation = %+v", low)
	}
	if _, err := pool.FindNotification(ctx, weak.ID, "candidate", domain.DecisionEvaluated); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("weak notification error = %v, want not found", err)
	}

	if err := handle(ctx, matchTask(closed, profile.Version)); err != nil {
		t.Fatalf("closed match error = %v", err)
	}
	if _, err := pool.FindEvaluationByKey(ctx, closed.ID, profile.Version, closed.ContentHash, match.RulesPrompt, match.RulesModel); err != nil {
		t.Fatalf("closed evaluation error = %v", err)
	}
	if _, err := pool.FindNotification(ctx, closed.ID, "candidate", domain.DecisionEvaluated); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("closed notification error = %v, want not found", err)
	}
}

func TestMatchThresholdComesFromRules(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	profile := studentProfile()
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	company := "Example"
	job := insertMatchJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Intern",
		Company:      &company,
		Locations:    []domain.Location{{Raw: "Remote", Remote: true}},
		Description:  "Build services in Go.",
		Status:       domain.JobOpen,
	})
	rules := match.DefaultRules()
	rules.Threshold = 101
	if err := matchHandler(pool, rules)(ctx, matchTask(job, profile.Version)); err != nil {
		t.Fatalf("match error = %v", err)
	}
	stored := requireEvaluation(t, ctx, pool, job, profile.Version)
	if stored.OverallScore == nil || *stored.OverallScore != 85 {
		t.Fatalf("evaluation = %+v", stored)
	}
	if _, err := pool.FindNotification(ctx, job.ID, "candidate", domain.DecisionEvaluated); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("notification error = %v, want not found", err)
	}
}

func matchHandler(pool *store.Pool, rules match.Rules) func(context.Context, domain.Task) error {
	return Handler(pool, source.Greenhouse{}, rules, "candidate", slog.New(slog.DiscardHandler))
}

func studentProfile() domain.CandidateProfile {
	return domain.CandidateProfile{
		Name:               "candidate-" + domain.NewID(),
		Version:            testVersion(),
		Active:             true,
		RoleSynonyms:       []string{"intern", "internship", "praksa", "staž"},
		PreferredRoles:     []string{"backend engineer"},
		PreferredSkills:    []string{"Go", "Linux"},
		PreferredLocations: []domain.Location{{Raw: "Serbia"}, {Raw: "Remote", Remote: true}},
		PreferredCompanies: []string{"Example"},
		ExcludedCompanies:  []string{"Excluded Co"},
		ExperienceLevel:    "student",
	}
}

func testVersion() int {
	version := int(time.Now().UnixNano() % 1_000_000_000)
	if version < 2 {
		return 2
	}
	return version
}

func insertMatchJob(t *testing.T, ctx context.Context, pool *store.Pool, job domain.Job) domain.Job {
	t.Helper()
	src := domain.JobSource{
		Name:    "match-" + domain.NewID(),
		Type:    domain.SourceGreenhouse,
		Enabled: true,
		Config:  domain.SourceConfig{BoardToken: "example"},
	}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if job.Kind == "" {
		job.Kind = domain.KindListing
	}
	if job.ApplyMethod == "" {
		job.ApplyMethod = domain.ApplyURL
	}
	if job.URL == "" {
		job.URL = "https://boards.example/jobs/" + domain.NewID()
	}
	if job.DiscoveredAt.IsZero() {
		job.DiscoveredAt = time.Now().UTC()
	}
	if job.ContentHash == "" {
		job.ContentHash = domain.NewID()
	}
	ref := domain.SourceReference{
		SourceID:     src.ID,
		ExternalID:   strPtr(domain.NewID()),
		OriginalURL:  job.URL,
		CanonicalURL: job.URL,
		DiscoveredAt: job.DiscoveredAt,
	}
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("InsertJobWithReference() error = %v", err)
	}
	return job
}

func matchTask(job domain.Job, version int) domain.Task {
	hash := job.ContentHash
	return domain.Task{
		Type:           domain.TaskMatch,
		JobID:          &job.ID,
		ContentHash:    &hash,
		ProfileVersion: &version,
	}
}

func requireEvaluation(t *testing.T, ctx context.Context, pool *store.Pool, job domain.Job, version int) domain.JobEvaluation {
	t.Helper()
	stored, err := pool.FindEvaluationByKey(ctx, job.ID, version, job.ContentHash, match.RulesPrompt, match.RulesModel)
	if err != nil {
		t.Fatalf("FindEvaluationByKey() error = %v", err)
	}
	return stored
}

func strPtr(value string) *string {
	return &value
}
