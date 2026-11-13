package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/match"
	"github.com/Lazzar19/ScoutAI/internal/pipeline"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func TestPreviewReplyBecomesAnEvaluatedMessage(t *testing.T) {
	pool := openNotifyPool(t)
	ctx := context.Background()
	userID, user := testUser(t)
	profile := notifyProfile(userID)
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	company := "Example"
	original := "https://boards.example/jobs/intern?utm_source=alert"
	job := insertNotifyJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessPreview,
		Title:        "Backend Intern",
		Company:      &company,
		Locations:    []domain.Location{{Raw: "Europe", Remote: true}},
		Status:       domain.JobOpen,
		URL:          "https://boards.example/jobs/intern",
	}, original)

	sends, server := telegramServer(t, http.StatusOK)
	defer server.Close()
	bot := testBot(pool, userID, server)
	handle := pipeline.Handler(pool, source.Greenhouse{}, match.DefaultRules(), userID, slog.New(slog.DiscardHandler))
	if err := handle(ctx, matchTask(job, profile.Version)); err != nil {
		t.Fatalf("preview match error = %v", err)
	}
	if err := bot.DeliverOnce(ctx); err != nil {
		t.Fatalf("DeliverOnce() error = %v", err)
	}
	if len(*sends) != 1 || !strings.Contains(sentText(t, (*sends)[0]), "PREVIEW — description not in the source") || !strings.Contains(sentText(t, (*sends)[0]), "utm_source=alert") {
		t.Fatalf("sends = %+v", *sends)
	}
	if _, ok := (*sends)[0]["parse_mode"]; ok {
		t.Fatal("parse_mode was set")
	}
	preview, err := pool.FindNotification(ctx, job.ID, userID, domain.DecisionPreview)
	if err != nil {
		t.Fatalf("FindNotification() error = %v", err)
	}
	if preview.Status != domain.DeliverySent || preview.ExternalMessageID == nil {
		t.Fatalf("preview notification = %+v", preview)
	}
	again := preview
	again.ID = ""
	again.Status = domain.DeliveryPending
	again.ExternalMessageID = nil
	again.SentAt = nil
	if err := pool.InsertNotification(ctx, &again); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second preview error = %v, want ErrConflict", err)
	}

	replyID, err := strconv.ParseInt(*preview.ExternalMessageID, 10, 64)
	if err != nil {
		t.Fatalf("message id = %s", *preview.ExternalMessageID)
	}
	if err := bot.handle(ctx, Update{Message: &Incoming{UserID: user, ChatID: user, Text: "Build services in Go.", ReplyID: replyID}}); err != nil {
		t.Fatalf("reply error = %v", err)
	}
	upgraded, err := pool.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if upgraded.Completeness != domain.CompletenessFull || upgraded.Description != "Build services in Go." || upgraded.URL != job.URL {
		t.Fatalf("upgraded job = %+v", upgraded)
	}
	task, err := pool.FindActiveMatch(ctx, upgraded.ID, upgraded.ContentHash, profile.Version)
	if err != nil {
		t.Fatalf("FindActiveMatch() error = %v", err)
	}
	if err := handle(ctx, task); err != nil {
		t.Fatalf("evaluated match error = %v", err)
	}
	if err := bot.DeliverOnce(ctx); err != nil {
		t.Fatalf("second DeliverOnce() error = %v", err)
	}
	if !hasText(*sends, "MATCH 85/100") {
		t.Fatalf("sends = %+v", *sends)
	}
	evaluated, err := pool.FindNotification(ctx, job.ID, userID, domain.DecisionEvaluated)
	if err != nil {
		t.Fatalf("evaluated notification error = %v", err)
	}
	if evaluated.Status != domain.DeliverySent {
		t.Fatalf("evaluated notification = %+v", evaluated)
	}
	before := len(*sends)
	if err := bot.DeliverOnce(ctx); err != nil {
		t.Fatalf("third DeliverOnce() error = %v", err)
	}
	if len(*sends) != before {
		t.Fatalf("sent %d extra messages", len(*sends)-before)
	}
}

func TestFeedbackDoesNotChangeTheScore(t *testing.T) {
	pool := openNotifyPool(t)
	ctx := context.Background()
	userID, user := testUser(t)
	profile := notifyProfile(userID)
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	company := "Example"
	job := insertNotifyJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Intern",
		Company:      &company,
		Locations:    []domain.Location{{Raw: "Europe", Remote: true}},
		Description:  "Build services in Go.",
		Status:       domain.JobOpen,
	}, "https://boards.example/jobs/intern")
	before, err := pool.GetProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	score := match.Decide(job, before, match.DefaultRules())
	note := domain.Notification{
		JobID:     job.ID,
		Recipient: userID,
		Type:      domain.DecisionEvaluated,
		Channel:   domain.ChannelTelegram,
		Status:    domain.DeliverySent,
	}
	if err := pool.InsertNotification(ctx, &note); err != nil {
		t.Fatalf("InsertNotification() error = %v", err)
	}
	sends, server := telegramServer(t, http.StatusOK)
	defer server.Close()
	bot := testBot(pool, userID, server)
	callback := Callback{ID: "cb-1", UserID: user, ChatID: user, Data: "fb:interesting:" + note.ID}
	if err := bot.handle(ctx, Update{Callback: &callback}); err != nil {
		t.Fatalf("callback error = %v", err)
	}
	if err := bot.handle(ctx, Update{Callback: &callback}); err != nil {
		t.Fatalf("second callback error = %v", err)
	}
	other := callback
	other.Data = "fb:not_relevant:" + note.ID
	if err := bot.handle(ctx, Update{Callback: &other}); err != nil {
		t.Fatalf("other callback error = %v", err)
	}
	choices, err := pool.ListFeedback(ctx, note.ID)
	if err != nil {
		t.Fatalf("ListFeedback() error = %v", err)
	}
	if len(choices) != 2 || choices[0].Type != domain.FeedbackInteresting || choices[1].Type != domain.FeedbackNotRelevant {
		t.Fatalf("feedback = %+v", choices)
	}
	after, err := pool.GetProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if after.Version != before.Version || len(after.PreferredSkills) != len(before.PreferredSkills) {
		t.Fatalf("profile changed from %+v to %+v", before, after)
	}
	again := match.Decide(job, after, match.DefaultRules())
	if again.Overall == nil || score.Overall == nil || *again.Overall != *score.Overall {
		t.Fatalf("score changed from %+v to %+v", score.Overall, again.Overall)
	}
	if len(*sends) != 0 {
		t.Fatalf("feedback sent a chat message: %+v", *sends)
	}

	if err := bot.handle(ctx, Update{Message: &Incoming{UserID: user + 1, ChatID: user + 1, Text: "/help"}}); err != nil {
		t.Fatalf("stranger error = %v", err)
	}
	if len(*sends) != 0 {
		t.Fatal("a stranger was answered")
	}
}

func TestTransientTelegramErrorStaysPending(t *testing.T) {
	pool := openNotifyPool(t)
	ctx := context.Background()
	userID, _ := testUser(t)
	job := insertNotifyJob(t, ctx, pool, domain.Job{
		Completeness: domain.CompletenessPreview,
		Title:        "Backend Intern",
		Status:       domain.JobOpen,
	}, "https://boards.example/jobs/retry")
	note := domain.Notification{
		JobID:     job.ID,
		Recipient: userID,
		Type:      domain.DecisionPreview,
		Channel:   domain.ChannelTelegram,
		Status:    domain.DeliveryPending,
	}
	if err := pool.InsertNotification(ctx, &note); err != nil {
		t.Fatalf("InsertNotification() error = %v", err)
	}
	_, server := telegramServer(t, http.StatusBadGateway)
	defer server.Close()
	bot := testBot(pool, userID, server)
	bot.MaxAttempts = 2
	if err := bot.DeliverOnce(ctx); err != nil {
		t.Fatalf("first DeliverOnce() error = %v", err)
	}
	pending, err := pool.GetNotification(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNotification() error = %v", err)
	}
	if pending.Status != domain.DeliveryPending || pending.Error == nil {
		t.Fatalf("notification = %+v", pending)
	}
	if err := bot.DeliverOnce(ctx); err != nil {
		t.Fatalf("second DeliverOnce() error = %v", err)
	}
	failed, err := pool.GetNotification(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNotification() error = %v", err)
	}
	if failed.Status != domain.DeliveryFailed {
		t.Fatalf("notification = %+v", failed)
	}
}

func telegramServer(t *testing.T, status int) (*[]map[string]any, *httptest.Server) {
	t.Helper()
	sends := &[]map[string]any{}
	nextID := time.Now().UnixNano() % 1_000_000_000_000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":502,"description":"down"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("body error = %v", err)
		}
		*sends = append(*sends, payload)
		nextID++
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":` + strconv.FormatInt(nextID, 10) + `}}`))
	}))
	return sends, server
}

func testBot(pool *store.Pool, userID string, server *httptest.Server) *Bot {
	return &Bot{
		Token:       "test-token",
		UserID:      userID,
		Store:       pool,
		Preview:     true,
		Evaluated:   true,
		RetryBase:   time.Nanosecond,
		MaxAttempts: 5,
		Logger:      slog.New(slog.DiscardHandler),
		Client:      Client{Token: "test-token", BaseURL: server.URL, HTTP: server.Client()},
	}
}

func testUser(t *testing.T) (string, int64) {
	t.Helper()
	id := time.Now().UnixNano()
	return strconv.FormatInt(id, 10), id
}

func notifyProfile(name string) domain.CandidateProfile {
	version := int(time.Now().UnixNano() % 1_000_000_000)
	if version < 2 {
		version = 2
	}
	return domain.CandidateProfile{
		Name:               "candidate-" + name,
		Version:            version,
		Active:             true,
		RoleSynonyms:       []string{"intern"},
		PreferredRoles:     []string{"backend engineer"},
		PreferredSkills:    []string{"Go", "Linux"},
		PreferredLocations: []domain.Location{{Raw: "Remote", Remote: true}, {Raw: "Europe"}},
		PreferredCompanies: []string{"Example"},
		ExperienceLevel:    "student",
	}
}

func insertNotifyJob(t *testing.T, ctx context.Context, pool *store.Pool, job domain.Job, original string) domain.Job {
	t.Helper()
	src := domain.JobSource{
		Name:    "Greenhouse-" + domain.NewID(),
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
		job.URL = original
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
		OriginalURL:  original,
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
	return domain.Task{Type: domain.TaskMatch, JobID: &job.ID, ContentHash: &hash, ProfileVersion: &version}
}

func sentText(t *testing.T, payload map[string]any) string {
	t.Helper()
	text, _ := payload["text"].(string)
	if text == "" {
		t.Fatalf("payload = %+v", payload)
	}
	return text
}

func hasText(sends []map[string]any, want string) bool {
	for _, payload := range sends {
		text, _ := payload["text"].(string)
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

func strPtr(value string) *string { return &value }

func openNotifyPool(t *testing.T) *store.Pool {
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
