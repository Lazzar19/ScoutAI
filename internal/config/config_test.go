package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PORT", "")
	t.Setenv("TELEGRAM_TOKEN", "")
	t.Setenv("MAILBOX_URL", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want a missing DATABASE_URL error")
	}
}

func TestLoadAllowsMissingFeatureSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://scout:scout@127.0.0.1:5432/scout?sslmode=disable")
	t.Setenv("PORT", "")
	t.Setenv("TELEGRAM_TOKEN", "")
	t.Setenv("MAILBOX_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("WORKER_COUNT", "")
	t.Setenv("TASK_TIMEOUT", "")
	t.Setenv("TASK_MAX_ATTEMPTS", "")
	t.Setenv("WORKER_POLL_INTERVAL", "")
	t.Setenv("TASK_RETRY_BASE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != "0.0.0.0:8080" {
		t.Fatalf("HTTPAddr = %q, want 0.0.0.0:8080", cfg.HTTPAddr)
	}
	if cfg.TelegramToken != "" || cfg.MailboxURL != "" || cfg.LLMAPIKey != "" {
		t.Fatal("feature secrets were set from an empty environment")
	}
	if cfg.WorkerCount != defaultWorkerCount || cfg.TaskMaxAttempts != defaultTaskMaxAttempts {
		t.Fatalf("worker defaults = %d attempts %d", cfg.WorkerCount, cfg.TaskMaxAttempts)
	}
	if cfg.TaskTimeout != defaultTaskTimeout || cfg.WorkerPoll != defaultWorkerPoll || cfg.TaskRetryBase != defaultTaskRetryBase {
		t.Fatalf("durations = %s %s %s", cfg.TaskTimeout, cfg.WorkerPoll, cfg.TaskRetryBase)
	}
}

func TestLoadRejectsBadWorkerSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://scout:scout@127.0.0.1:5432/scout?sslmode=disable")
	t.Setenv("PORT", "8080")

	t.Setenv("WORKER_COUNT", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an invalid WORKER_COUNT error")
	}

	t.Setenv("WORKER_COUNT", "1")
	t.Setenv("TASK_TIMEOUT", "soon")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an invalid TASK_TIMEOUT error")
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://scout:scout@127.0.0.1:5432/scout?sslmode=disable")
	t.Setenv("PORT", "not-a-port")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want an invalid PORT error")
	}
}
