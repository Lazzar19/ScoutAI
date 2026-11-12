package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/match"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func TestGreenhouseSnapshotStoresAndCloses(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	profile := domain.CandidateProfile{Name: "candidate-" + domain.NewID(), Version: 1, Active: true}
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	src := domain.JobSource{
		Name:    "greenhouse-" + domain.NewID(),
		Type:    domain.SourceGreenhouse,
		Enabled: true,
		Config:  domain.SourceConfig{BoardToken: "example"},
	}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}

	body := board(
		jobJSON("1", "Systems Software Intern", "Build systems. &lt;p&gt;Go and Linux&lt;/p&gt;"),
		jobJSON("2", "Systems Software Intern", "Backend intern. &lt;p&gt;Rust&lt;/p&gt;"),
	)
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "content=true") {
			t.Errorf("query = %s, want content=true", r.URL.RawQuery)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	boards := source.Greenhouse{BaseURL: server.URL, Client: server.Client()}
	handle := Handler(pool, boards, match.DefaultRules(), "candidate", slog.New(slog.DiscardHandler))
	task := domain.Task{Type: domain.TaskPoll, SourceID: &src.ID}

	if err := handle(ctx, task); err != nil {
		t.Fatalf("first poll error = %v", err)
	}
	first := requireJob(t, ctx, pool, src.ID, "1")
	second := requireJob(t, ctx, pool, src.ID, "2")
	if first.ID == second.ID {
		t.Fatal("two postings with the same title were stored as one job")
	}
	if first.Completeness != domain.CompletenessFull || !strings.Contains(first.Description, "Go and Linux") || strings.Contains(first.Description, "<p>") {
		t.Fatalf("description = %q", first.Description)
	}
	if first.Locations == nil || !first.Locations[0].Remote {
		t.Fatalf("locations = %+v", first.Locations)
	}
	if _, err := pool.FindActiveMatch(ctx, first.ID, first.ContentHash, profile.Version); err != nil {
		t.Fatalf("FindActiveMatch() error = %v", err)
	}

	if err := handle(ctx, task); err != nil {
		t.Fatalf("second poll error = %v", err)
	}
	again := requireJob(t, ctx, pool, src.ID, "1")
	if again.ID != first.ID {
		t.Fatalf("job id = %s, want the original %s", again.ID, first.ID)
	}

	body = board(jobJSON("1", "Systems Software Intern", "Build systems. &lt;p&gt;Go and Linux&lt;/p&gt;"))
	if err := handle(ctx, task); err != nil {
		t.Fatalf("narrow poll error = %v", err)
	}
	closed := requireJob(t, ctx, pool, src.ID, "2")
	if closed.Status != domain.JobClosed {
		t.Fatalf("missing job status = %s, want closed", closed.Status)
	}
	if requireJob(t, ctx, pool, src.ID, "1").Status != domain.JobOpen {
		t.Fatal("the job still on the board was closed")
	}

	status = http.StatusBadGateway
	if err := handle(ctx, task); err == nil {
		t.Fatal("failed poll error = nil")
	}
	if requireJob(t, ctx, pool, src.ID, "1").Status != domain.JobOpen {
		t.Fatal("a failed fetch closed an open job")
	}

	status = http.StatusOK
	body = board(
		jobJSON("3", "Platform Intern", "Kubernetes. &lt;p&gt;Linux&lt;/p&gt;"),
		`{"id":9,"title":"","absolute_url":"https://boards.example/jobs/9","content":"missing title"}`,
	)
	if err := handle(ctx, task); err != nil {
		t.Fatalf("partial poll error = %v", err)
	}
	kept := requireJob(t, ctx, pool, src.ID, "1")
	if kept.Status != domain.JobOpen {
		t.Fatal("a bad posting caused an open job to close")
	}
	added := requireJob(t, ctx, pool, src.ID, "3")
	if added.Completeness != domain.CompletenessFull || !strings.Contains(added.Description, "Linux") {
		t.Fatalf("kept posting = %+v", added)
	}
	if _, err := pool.FindReferenceByExternalID(ctx, src.ID, "9"); err == nil {
		t.Fatal("the empty title was stored")
	}
}

func TestCanonicalURLDedupesWhenExternalIDIsMissing(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	src := domain.JobSource{
		Name:    "url-only-" + domain.NewID(),
		Type:    domain.SourceEmail,
		Enabled: true,
	}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	posting := source.DiscoveredPosting{
		Title:        "Backend Intern",
		OriginalURL:  "https://boards.example/jobs/abc?utm_source=alert",
		CanonicalURL: "https://boards.example/jobs/abc?utm_source=alert",
		Kind:         domain.KindPost,
		Completeness: domain.CompletenessPreview,
		Description:  "preview text",
	}
	snapshot := source.Snapshot{Postings: []source.DiscoveredPosting{posting}}
	if _, err := Apply(ctx, pool, src, snapshot, time.Now().UTC()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	posting.Description = "updated preview"
	snapshot.Postings[0] = posting
	if _, err := Apply(ctx, pool, src, snapshot, time.Now().UTC()); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	ref, err := pool.FindReferenceByCanonicalURL(ctx, src.ID, "https://boards.example/jobs/abc")
	if err != nil {
		t.Fatalf("FindReferenceByCanonicalURL() error = %v", err)
	}
	job, err := pool.GetJob(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if job.URL != "https://boards.example/jobs/abc" || job.Description != "updated preview" {
		t.Fatalf("job = %s %q", job.URL, job.Description)
	}
}

func board(jobs ...string) string {
	return `{"jobs":[` + strings.Join(jobs, ",") + `]}`
}

func jobJSON(id, title, content string) string {
	return fmt.Sprintf(
		`{"id":%s,"title":%q,"absolute_url":"https://boards.example/jobs/%s?utm_source=alert","language":"en","content":%q,"location":{"name":"Remote, Europe"}}`,
		id, title, id, content,
	)
}

func requireJob(t *testing.T, ctx context.Context, pool *store.Pool, sourceID, externalID string) domain.Job {
	t.Helper()
	ref, err := pool.FindReferenceByExternalID(ctx, sourceID, externalID)
	if err != nil {
		t.Fatalf("FindReferenceByExternalID(%s) error = %v", externalID, err)
	}
	job, err := pool.GetJob(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	return job
}

func openPool(t *testing.T) *store.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.Migrate(ctx, databaseURL, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
