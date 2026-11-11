package store

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func TestTablesExist(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	names := []string{
		"sources",
		"raw_messages",
		"jobs",
		"job_source_references",
		"candidate_profiles",
		"job_evaluations",
		"notifications",
		"processing_jobs",
		"source_runs",
		"user_feedback",
	}
	for _, name := range names {
		var regclass *string
		if err := pool.pool.QueryRow(ctx, `SELECT to_regclass($1)`, "public."+name).Scan(&regclass); err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		if regclass == nil {
			t.Fatalf("table %s does not exist", name)
		}
	}
}

func TestJobAndReferencePersistTogether(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())

	job := sampleJob("hash-together")
	ref := sampleRef(source.ID, "ext-1", "https://boards.example/jobs/1", job.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("InsertJobWithReference() error = %v", err)
	}

	gotJob, err := pool.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	assertJob(t, gotJob, job)

	gotRef, err := pool.GetSourceReference(ctx, ref.ID)
	if err != nil {
		t.Fatalf("GetSourceReference() error = %v", err)
	}
	if gotRef.JobID != job.ID || gotRef.SourceID != source.ID || !sameString(gotRef.ExternalID, ref.ExternalID) {
		t.Fatalf("reference = %+v, want job %s source %s", gotRef, job.ID, source.ID)
	}
	if gotRef.CanonicalURL != ref.CanonicalURL || gotRef.OriginalURL != ref.OriginalURL {
		t.Fatalf("reference urls = %s %s", gotRef.OriginalURL, gotRef.CanonicalURL)
	}
	if !gotRef.DiscoveredAt.Equal(ref.DiscoveredAt) {
		t.Fatalf("reference discovered_at = %s, want %s", gotRef.DiscoveredAt, ref.DiscoveredAt)
	}

	gotSource, err := pool.GetSource(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if gotSource.Name != source.Name || gotSource.Config.BoardToken != "example" || !gotSource.Enabled {
		t.Fatalf("source = %+v", gotSource)
	}
}

func TestDuplicateExternalIDRollsBackTheJob(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())

	first := sampleJob("hash-ext-a")
	firstRef := sampleRef(source.ID, "same-ext", "https://boards.example/jobs/a", first.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &first, &firstRef); err != nil {
		t.Fatalf("first insert error = %v", err)
	}

	second := sampleJob("hash-ext-b")
	second.URL = "https://boards.example/jobs/b"
	secondRef := sampleRef(source.ID, "same-ext", "https://boards.example/jobs/b", second.DiscoveredAt)
	err := pool.InsertJobWithReference(ctx, &second, &secondRef)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second insert error = %v, want ErrConflict", err)
	}

	_, err = pool.GetJob(ctx, second.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetJob(rolled back) error = %v, want ErrNotFound", err)
	}
	if _, err := pool.GetJob(ctx, first.ID); err != nil {
		t.Fatalf("first job was not kept: %v", err)
	}
}

func TestCanonicalURLFallbackRejectsASecondRow(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "mail-"+domain.NewID())
	canonical := "https://example.com/jobs/" + domain.NewID()

	first := sampleJob("hash-url-a")
	firstRef := sampleRef(source.ID, "", canonical, first.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &first, &firstRef); err != nil {
		t.Fatalf("first insert error = %v", err)
	}

	second := sampleJob("hash-url-b")
	second.URL = canonical + "/other"
	secondRef := sampleRef(source.ID, "", canonical, second.DiscoveredAt)
	err := pool.InsertJobWithReference(ctx, &second, &secondRef)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second insert error = %v, want ErrConflict", err)
	}
	if _, err := pool.GetJob(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetJob(rolled back) error = %v, want ErrNotFound", err)
	}
}

func TestSameCanonicalURLWithDifferentExternalIDsIsAllowed(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	canonical := "https://boards.example/jobs/shared"

	first := sampleJob("hash-shared-a")
	firstRef := sampleRef(source.ID, "ext-a", canonical, first.DiscoveredAt)
	second := sampleJob("hash-shared-b")
	second.URL = "https://boards.example/jobs/shared-b"
	secondRef := sampleRef(source.ID, "ext-b", canonical, second.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &first, &firstRef); err != nil {
		t.Fatalf("first insert error = %v", err)
	}
	if err := pool.InsertJobWithReference(ctx, &second, &secondRef); err != nil {
		t.Fatalf("second insert error = %v, want success when external ids differ", err)
	}
}

func TestContentHashCanRepeat(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	const hash = "same-description-hash"

	first := sampleJob(hash)
	firstRef := sampleRef(source.ID, "hash-ext-a", "https://boards.example/jobs/hash-a", first.DiscoveredAt)
	second := sampleJob(hash)
	second.Title = "Backend Intern"
	second.URL = "https://boards.example/jobs/hash-b"
	secondRef := sampleRef(source.ID, "hash-ext-b", "https://boards.example/jobs/hash-b", second.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &first, &firstRef); err != nil {
		t.Fatalf("first insert error = %v", err)
	}
	if err := pool.InsertJobWithReference(ctx, &second, &secondRef); err != nil {
		t.Fatalf("second insert error = %v, want a repeated content hash to be stored", err)
	}

	got, err := pool.GetJob(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if got.ContentHash != hash || got.Title != "Backend Intern" {
		t.Fatalf("stored job = %+v", got)
	}
}

func TestNotificationTypeCannotBeInsertedTwice(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	job := sampleJob("hash-notify")
	ref := sampleRef(source.ID, "notify-ext", "https://boards.example/jobs/notify", job.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("insert job error = %v", err)
	}

	preview := domain.Notification{
		JobID:     job.ID,
		Recipient: "42",
		Type:      domain.DecisionPreview,
		Channel:   domain.ChannelTelegram,
		Status:    domain.DeliveryPending,
	}
	if err := pool.InsertNotification(ctx, &preview); err != nil {
		t.Fatalf("insert preview error = %v", err)
	}
	again := preview
	again.ID = ""
	if err := pool.InsertNotification(ctx, &again); !errors.Is(err, ErrConflict) {
		t.Fatalf("second preview error = %v, want ErrConflict", err)
	}

	evaluated := preview
	evaluated.ID = ""
	evaluated.Type = domain.DecisionEvaluated
	if err := pool.InsertNotification(ctx, &evaluated); err != nil {
		t.Fatalf("evaluated insert error = %v, want a second type to be stored", err)
	}
	got, err := pool.GetNotification(ctx, preview.ID)
	if err != nil {
		t.Fatalf("GetNotification() error = %v", err)
	}
	if got.Type != domain.DecisionPreview || got.Status != domain.DeliveryPending || got.Recipient != "42" {
		t.Fatalf("notification = %+v", got)
	}
}

func TestProfileVersionBumpDoesNotCollide(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	job := sampleJob("hash-profile")
	ref := sampleRef(source.ID, "profile-ext", "https://boards.example/jobs/profile", job.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("insert job error = %v", err)
	}
	profile := sampleProfile("candidate", 1)
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}

	first := sampleEvaluation(job.ID, profile.ID, job.ContentHash, 1)
	if err := pool.InsertEvaluation(ctx, &first); err != nil {
		t.Fatalf("first evaluation error = %v", err)
	}
	duplicate := first
	duplicate.ID = ""
	if err := pool.InsertEvaluation(ctx, &duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate evaluation error = %v, want ErrConflict", err)
	}

	profile.Version = 2
	profile.PreferredSkills = []string{"Go", "Rust"}
	if err := pool.UpdateProfile(ctx, profile); err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	stored, err := pool.GetProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if stored.Version != 2 || len(stored.PreferredSkills) != 2 || stored.PreferredSkills[1] != "Rust" {
		t.Fatalf("profile = %+v", stored)
	}
	if len(stored.RoleSynonyms) != 2 || stored.PreferredLocations[0].City == nil || *stored.PreferredLocations[0].City != "Belgrade" {
		t.Fatalf("profile preferences = %+v", stored)
	}

	second := sampleEvaluation(job.ID, profile.ID, job.ContentHash, 2)
	second.Kind = domain.DecisionPreview
	second.OverallScore = nil
	second.EvaluationData = nil
	if err := pool.InsertEvaluation(ctx, &second); err != nil {
		t.Fatalf("version 2 evaluation error = %v, want a new profile version to be stored", err)
	}
	got, err := pool.GetEvaluation(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetEvaluation() error = %v", err)
	}
	if got.ProfileVersion != 2 || got.OverallScore != nil || got.Kind != domain.DecisionPreview {
		t.Fatalf("evaluation = %+v", got)
	}
	if len(got.EvaluationData) != 0 {
		t.Fatalf("evaluation data = %s, want empty", got.EvaluationData)
	}
	previous, err := pool.GetEvaluation(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetEvaluation(version 1) error = %v", err)
	}
	if previous.ProfileVersion != 1 || previous.OverallScore == nil || *previous.OverallScore != 84 {
		t.Fatalf("version 1 evaluation = %+v", previous)
	}
}

func TestActiveMatchTaskRejectsTheSameKey(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	job := sampleJob("hash-task")
	ref := sampleRef(source.ID, "task-ext", "https://boards.example/jobs/task", job.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("insert job error = %v", err)
	}

	pending := matchTask(job, domain.TaskPending)
	if err := pool.InsertTask(ctx, &pending); err != nil {
		t.Fatalf("insert pending error = %v", err)
	}
	again := matchTask(job, domain.TaskPending)
	if err := pool.InsertTask(ctx, &again); !errors.Is(err, ErrConflict) {
		t.Fatalf("second pending error = %v, want ErrConflict", err)
	}

	changed := matchTask(job, domain.TaskPending)
	otherHash := "other-hash"
	changed.ContentHash = &otherHash
	if err := pool.InsertTask(ctx, &changed); err != nil {
		t.Fatalf("changed hash error = %v, want a new content hash to be queued", err)
	}

	if _, err := pool.GetTask(ctx, pending.ID); err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
}

func TestCompletedMatchDoesNotBlockTheNextTask(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	job := sampleJob("hash-task-done")
	ref := sampleRef(source.ID, "task-done-ext", "https://boards.example/jobs/task-done", job.DiscoveredAt)
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("insert job error = %v", err)
	}

	done := matchTask(job, domain.TaskCompleted)
	if err := pool.InsertTask(ctx, &done); err != nil {
		t.Fatalf("completed insert error = %v", err)
	}
	pending := matchTask(job, domain.TaskPending)
	if err := pool.InsertTask(ctx, &pending); err != nil {
		t.Fatalf("pending after completed error = %v", err)
	}
}

func TestActivePollTaskIsUniquePerSource(t *testing.T) {
	pool := openTestPool(t)
	ctx := testContext(t)
	source := insertSource(t, ctx, pool, "greenhouse-"+domain.NewID())
	available := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)

	first := domain.Task{Type: domain.TaskPoll, SourceID: &source.ID, Status: domain.TaskPending, AvailableAt: available}
	if err := pool.InsertTask(ctx, &first); err != nil {
		t.Fatalf("first poll error = %v", err)
	}
	second := domain.Task{Type: domain.TaskPoll, SourceID: &source.ID, Status: domain.TaskProcessing, AvailableAt: available}
	if err := pool.InsertTask(ctx, &second); !errors.Is(err, ErrConflict) {
		t.Fatalf("second poll error = %v, want ErrConflict", err)
	}
	later := domain.Task{Type: domain.TaskPoll, SourceID: &source.ID, Status: domain.TaskCompleted, AvailableAt: available}
	if err := pool.InsertTask(ctx, &later); err != nil {
		t.Fatalf("completed poll error = %v, want a finished poll to be stored", err)
	}
}

func openTestPool(t *testing.T) *Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Migrate(ctx, databaseURL, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func insertSource(t *testing.T, ctx context.Context, pool *Pool, name string) domain.JobSource {
	t.Helper()
	source := domain.JobSource{
		Name:    name,
		Type:    domain.SourceGreenhouse,
		Enabled: true,
		Config:  domain.SourceConfig{BoardToken: "example"},
	}
	if err := pool.InsertSource(ctx, &source); err != nil {
		t.Fatalf("InsertSource() error = %v", err)
	}
	return source
}

func sampleJob(hash string) domain.Job {
	company := "Example"
	city := "Novi Sad"
	posted := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	return domain.Job{
		Kind:           domain.KindListing,
		Completeness:   domain.CompletenessFull,
		Title:          "Systems Software Intern",
		Company:        &company,
		Locations:      []domain.Location{{City: &city, Remote: true, Raw: "Novi Sad / Remote"}},
		Description:    "Intern role working on distributed systems.",
		URL:            "https://boards.example/jobs/" + hash,
		ApplyMethod:    domain.ApplyURL,
		EmploymentType: "intern",
		Experience:     "student",
		PostedAt:       &posted,
		DiscoveredAt:   time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		ContentHash:    hash,
		Status:         domain.JobOpen,
	}
}

func sampleRef(sourceID, externalID, canonical string, discovered time.Time) domain.SourceReference {
	ref := domain.SourceReference{
		SourceID:     sourceID,
		OriginalURL:  canonical + "?utm_source=alert",
		CanonicalURL: canonical,
		DiscoveredAt: discovered,
	}
	if externalID != "" {
		ref.ExternalID = &externalID
	}
	return ref
}

func sampleProfile(name string, version int) domain.CandidateProfile {
	return domain.CandidateProfile{
		Name:               name,
		Version:            version,
		Active:             true,
		PreferredRoles:     []string{"Backend Engineering Intern"},
		RoleSynonyms:       []string{"intern", "praksa"},
		PreferredSkills:    []string{"Go"},
		AdditionalSkills:   []string{"Python"},
		PreferredLocations: []domain.Location{{City: strPtr("Belgrade"), Country: strPtr("Serbia")}},
		PreferredCompanies: []string{"Example"},
		ExcludedCompanies:  []string{"Excluded"},
		ExcludedRoles:      []string{"Staff Engineer"},
		ExperienceLevel:    "student",
	}
}

func sampleEvaluation(jobID, profileID, hash string, version int) domain.JobEvaluation {
	score := 84
	return domain.JobEvaluation{
		JobID:          jobID,
		ProfileID:      profileID,
		ProfileVersion: version,
		ContentHash:    hash,
		PromptVersion:  "rules-v1",
		Model:          "rules",
		Kind:           domain.DecisionEvaluated,
		OverallScore:   &score,
		EvaluationData: []byte(`{"role_category":"systems_engineering"}`),
		EvaluatedAt:    time.Date(2026, 11, 10, 13, 0, 0, 0, time.UTC),
	}
}

func matchTask(job domain.Job, status domain.TaskStatus) domain.Task {
	version := 1
	hash := job.ContentHash
	jobID := job.ID
	return domain.Task{
		Type:           domain.TaskMatch,
		JobID:          &jobID,
		ContentHash:    &hash,
		ProfileVersion: &version,
		Status:         status,
		AvailableAt:    time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC),
	}
}

func assertJob(t *testing.T, got, want domain.Job) {
	t.Helper()
	if got.ID != want.ID || got.Kind != want.Kind || got.Completeness != want.Completeness {
		t.Fatalf("identity = %s %s %s", got.ID, got.Kind, got.Completeness)
	}
	if got.Title != want.Title || !sameString(got.Company, want.Company) || got.Description != want.Description {
		t.Fatalf("text fields = %+v", got)
	}
	if got.URL != want.URL || got.ApplyMethod != want.ApplyMethod || got.Status != want.Status || got.ContentHash != want.ContentHash {
		t.Fatalf("keys = %+v", got)
	}
	if !sameTime(got.PostedAt, want.PostedAt) || !got.DiscoveredAt.Equal(want.DiscoveredAt) {
		t.Fatalf("times posted=%v discovered=%s", got.PostedAt, got.DiscoveredAt)
	}
	if len(got.Locations) != 1 || !sameString(got.Locations[0].City, want.Locations[0].City) || !got.Locations[0].Remote {
		t.Fatalf("locations = %+v", got.Locations)
	}
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func strPtr(value string) *string {
	return &value
}
