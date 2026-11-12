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
	t.Setenv("GREENHOUSE_INTERVAL", "")
	t.Setenv("GREENHOUSE_JITTER", "")
	t.Setenv("GREENHOUSE_API_BASE", "")
	t.Setenv("SOURCES", "")
	t.Setenv("MATCH_WEIGHT_ROLE", "")
	t.Setenv("MATCH_WEIGHT_SKILLS", "")
	t.Setenv("MATCH_WEIGHT_EXPERIENCE", "")
	t.Setenv("MATCH_WEIGHT_LOCATION", "")
	t.Setenv("MATCH_WEIGHT_COMPANY", "")
	t.Setenv("MATCH_THRESHOLD", "")
	t.Setenv("TELEGRAM_USER_ID", "")

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
	if cfg.GreenhouseInterval != defaultGreenhouseEvery || cfg.GreenhouseJitter != defaultGreenhouseJitter {
		t.Fatalf("greenhouse interval = %s jitter = %s", cfg.GreenhouseInterval, cfg.GreenhouseJitter)
	}
	if len(cfg.Sources) != 0 || cfg.GreenhouseBaseURL != defaultGreenhouseBaseURL {
		t.Fatalf("sources = %+v base = %s", cfg.Sources, cfg.GreenhouseBaseURL)
	}
	if cfg.MatchWeightRole != defaultMatchRole || cfg.MatchWeightSkills != defaultMatchSkills || cfg.MatchWeightExperience != defaultMatchExperience {
		t.Fatalf("match weights = %d %d %d", cfg.MatchWeightRole, cfg.MatchWeightSkills, cfg.MatchWeightExperience)
	}
	if cfg.MatchWeightLocation != defaultMatchLocation || cfg.MatchWeightCompany != defaultMatchCompany || cfg.MatchThreshold != defaultMatchThreshold {
		t.Fatalf("match location = %d company = %d threshold = %d", cfg.MatchWeightLocation, cfg.MatchWeightCompany, cfg.MatchThreshold)
	}
	if cfg.NotifyRecipient != defaultNotifyRecipient {
		t.Fatalf("recipient = %q", cfg.NotifyRecipient)
	}
}

func TestLoadParsesGreenhouseSources(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://scout:scout@127.0.0.1:5432/scout?sslmode=disable")
	t.Setenv("PORT", "")
	t.Setenv("SOURCES", `[{"name":"greenhouse-example","type":"greenhouse","board_token":"example"}]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0].BoardToken != "example" || !cfg.Sources[0].Enabled {
		t.Fatalf("sources = %+v", cfg.Sources)
	}

	t.Setenv("SOURCES", `[{"name":"greenhouse-example","type":"greenhouse"}]`)
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want a missing board token error")
	}
}

func TestLoadParsesMatchSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://scout:scout@127.0.0.1:5432/scout?sslmode=disable")
	t.Setenv("PORT", "")
	t.Setenv("MATCH_WEIGHT_ROLE", "40")
	t.Setenv("MATCH_WEIGHT_SKILLS", "20")
	t.Setenv("MATCH_WEIGHT_EXPERIENCE", "20")
	t.Setenv("MATCH_WEIGHT_LOCATION", "10")
	t.Setenv("MATCH_WEIGHT_COMPANY", "10")
	t.Setenv("MATCH_THRESHOLD", "80")
	t.Setenv("TELEGRAM_USER_ID", "42")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MatchWeightRole != 40 || cfg.MatchThreshold != 80 || cfg.NotifyRecipient != "42" {
		t.Fatalf("match = role %d threshold %d recipient %s", cfg.MatchWeightRole, cfg.MatchThreshold, cfg.NotifyRecipient)
	}

	t.Setenv("MATCH_THRESHOLD", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an invalid threshold error")
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
