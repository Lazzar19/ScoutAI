// Package config loads process settings from the environment.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWorkerCount       = 1
	defaultTaskTimeout       = 2 * time.Minute
	defaultTaskMaxAttempts   = 5
	defaultWorkerPoll        = 2 * time.Second
	defaultTaskRetryBase     = 5 * time.Second
	defaultGreenhouseEvery   = time.Hour
	defaultGreenhouseJitter  = 10 * time.Minute
	defaultGreenhouseBaseURL = "https://boards-api.greenhouse.io"
	defaultMatchRole         = 30
	defaultMatchSkills       = 30
	defaultMatchExperience   = 20
	defaultMatchLocation     = 10
	defaultMatchCompany      = 10
	defaultMatchThreshold    = 70
	defaultNotifyRecipient   = "candidate"
)

// Config is the process configuration. Only DatabaseURL is required.
// Telegram, mailbox, and model credentials stay empty until those features are enabled.
// Worker settings have defaults so the queue can run before those features exist.
type Config struct {
	DatabaseURL           string
	HTTPAddr              string
	TelegramToken         string
	MailboxURL            string
	LLMAPIKey             string
	WorkerCount           int
	TaskTimeout           time.Duration
	TaskMaxAttempts       int
	WorkerPoll            time.Duration
	TaskRetryBase         time.Duration
	GreenhouseInterval    time.Duration
	GreenhouseJitter      time.Duration
	GreenhouseBaseURL     string
	Sources               []ConfiguredSource
	MatchWeightRole       int
	MatchWeightSkills     int
	MatchWeightExperience int
	MatchWeightLocation   int
	MatchWeightCompany    int
	MatchThreshold        int
	NotifyRecipient       string
	NotifyPreview         bool
	NotifyEvaluated       bool
	TelegramAPIBase       string
}

// ConfiguredSource is one board or mailbox from the SOURCES environment value.
type ConfiguredSource struct {
	Name       string
	Type       string
	Enabled    bool
	BoardToken string
	Site       string
	SecretEnv  string
}

// Load reads configuration from the environment.
// Startup fails when DATABASE_URL is missing. The other credentials are optional.
func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return Config{}, fmt.Errorf("PORT must be a TCP port: %w", err)
	}

	workerCount, err := envPositiveInt("WORKER_COUNT", defaultWorkerCount)
	if err != nil {
		return Config{}, err
	}
	taskTimeout, err := envPositiveDuration("TASK_TIMEOUT", defaultTaskTimeout)
	if err != nil {
		return Config{}, err
	}
	maxAttempts, err := envPositiveInt("TASK_MAX_ATTEMPTS", defaultTaskMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	workerPoll, err := envPositiveDuration("WORKER_POLL_INTERVAL", defaultWorkerPoll)
	if err != nil {
		return Config{}, err
	}
	retryBase, err := envPositiveDuration("TASK_RETRY_BASE", defaultTaskRetryBase)
	if err != nil {
		return Config{}, err
	}
	greenhouseEvery, err := envPositiveDuration("GREENHOUSE_INTERVAL", defaultGreenhouseEvery)
	if err != nil {
		return Config{}, err
	}
	greenhouseJitter, err := envPositiveDuration("GREENHOUSE_JITTER", defaultGreenhouseJitter)
	if err != nil {
		return Config{}, err
	}
	sources, err := parseSources(os.Getenv("SOURCES"))
	if err != nil {
		return Config{}, err
	}
	baseURL := strings.TrimSpace(os.Getenv("GREENHOUSE_API_BASE"))
	if baseURL == "" {
		baseURL = defaultGreenhouseBaseURL
	}
	roleWeight, err := envNonNegativeInt("MATCH_WEIGHT_ROLE", defaultMatchRole)
	if err != nil {
		return Config{}, err
	}
	skillsWeight, err := envNonNegativeInt("MATCH_WEIGHT_SKILLS", defaultMatchSkills)
	if err != nil {
		return Config{}, err
	}
	experienceWeight, err := envNonNegativeInt("MATCH_WEIGHT_EXPERIENCE", defaultMatchExperience)
	if err != nil {
		return Config{}, err
	}
	locationWeight, err := envNonNegativeInt("MATCH_WEIGHT_LOCATION", defaultMatchLocation)
	if err != nil {
		return Config{}, err
	}
	companyWeight, err := envNonNegativeInt("MATCH_WEIGHT_COMPANY", defaultMatchCompany)
	if err != nil {
		return Config{}, err
	}
	threshold, err := envNonNegativeInt("MATCH_THRESHOLD", defaultMatchThreshold)
	if err != nil {
		return Config{}, err
	}
	token := strings.TrimSpace(os.Getenv("TELEGRAM_TOKEN"))
	userID := strings.TrimSpace(os.Getenv("TELEGRAM_USER_ID"))
	if token != "" && userID == "" {
		return Config{}, fmt.Errorf("TELEGRAM_USER_ID is required when TELEGRAM_TOKEN is set")
	}
	recipient := userID
	if recipient == "" {
		recipient = defaultNotifyRecipient
	}
	notifyPreview, err := envBool("NOTIFY_PREVIEW", true)
	if err != nil {
		return Config{}, err
	}
	notifyEvaluated, err := envBool("NOTIFY_EVALUATED", true)
	if err != nil {
		return Config{}, err
	}
	telegramBase := strings.TrimRight(strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE")), "/")
	if telegramBase == "" {
		telegramBase = "https://api.telegram.org"
	}

	return Config{
		DatabaseURL:           databaseURL,
		HTTPAddr:              net.JoinHostPort("0.0.0.0", port),
		TelegramToken:         token,
		MailboxURL:            strings.TrimSpace(os.Getenv("MAILBOX_URL")),
		LLMAPIKey:             strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		WorkerCount:           workerCount,
		TaskTimeout:           taskTimeout,
		TaskMaxAttempts:       maxAttempts,
		WorkerPoll:            workerPoll,
		TaskRetryBase:         retryBase,
		GreenhouseInterval:    greenhouseEvery,
		GreenhouseJitter:      greenhouseJitter,
		GreenhouseBaseURL:     baseURL,
		Sources:               sources,
		MatchWeightRole:       roleWeight,
		MatchWeightSkills:     skillsWeight,
		MatchWeightExperience: experienceWeight,
		MatchWeightLocation:   locationWeight,
		MatchWeightCompany:    companyWeight,
		MatchThreshold:        threshold,
		NotifyRecipient:       recipient,
		NotifyPreview:         notifyPreview,
		NotifyEvaluated:       notifyEvaluated,
		TelegramAPIBase:       telegramBase,
	}, nil
}

func parseSources(raw string) ([]ConfiguredSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var rows []struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Enabled    *bool  `json:"enabled"`
		BoardToken string `json:"board_token"`
		Site       string `json:"site"`
		SecretEnv  string `json:"secret_env"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, fmt.Errorf("SOURCES must be a JSON array: %w", err)
	}
	seen := map[string]bool{}
	sources := make([]ConfiguredSource, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		kind := strings.TrimSpace(row.Type)
		if name == "" || kind == "" {
			return nil, fmt.Errorf("SOURCES entries need a name and a type")
		}
		if seen[name] {
			return nil, fmt.Errorf("SOURCES name %s is duplicated", name)
		}
		seen[name] = true
		if kind == "greenhouse" && strings.TrimSpace(row.BoardToken) == "" {
			return nil, fmt.Errorf("SOURCES greenhouse source %s needs a board_token", name)
		}
		enabled := true
		if row.Enabled != nil {
			enabled = *row.Enabled
		}
		sources = append(sources, ConfiguredSource{
			Name:       name,
			Type:       kind,
			Enabled:    enabled,
			BoardToken: strings.TrimSpace(row.BoardToken),
			Site:       strings.TrimSpace(row.Site),
			SecretEnv:  strings.TrimSpace(row.SecretEnv),
		})
	}
	return sources, nil
}

func envPositiveInt(key string, fallback int) (int, error) {
	return envInt(key, fallback, false)
}

func envNonNegativeInt(key string, fallback int) (int, error) {
	return envInt(key, fallback, true)
}

func envInt(key string, fallback int, allowZero bool) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || (n == 0 && !allowZero) {
		if allowZero {
			return 0, fmt.Errorf("%s must be a non-negative integer", key)
		}
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback, nil
	}
	switch raw {
	case "1", "true", "yes", "y":
		return true, nil
	case "0", "false", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", key)
	}
}

func envPositiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}
