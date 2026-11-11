// Package config loads process settings from the environment.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWorkerCount     = 1
	defaultTaskTimeout     = 2 * time.Minute
	defaultTaskMaxAttempts = 5
	defaultWorkerPoll      = 2 * time.Second
	defaultTaskRetryBase   = 5 * time.Second
)

// Config is the process configuration. Only DatabaseURL is required.
// Telegram, mailbox, and model credentials stay empty until those features are enabled.
// Worker settings have defaults so the queue can run before those features exist.
type Config struct {
	DatabaseURL     string
	HTTPAddr        string
	TelegramToken   string
	MailboxURL      string
	LLMAPIKey       string
	WorkerCount     int
	TaskTimeout     time.Duration
	TaskMaxAttempts int
	WorkerPoll      time.Duration
	TaskRetryBase   time.Duration
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

	return Config{
		DatabaseURL:     databaseURL,
		HTTPAddr:        net.JoinHostPort("0.0.0.0", port),
		TelegramToken:   strings.TrimSpace(os.Getenv("TELEGRAM_TOKEN")),
		MailboxURL:      strings.TrimSpace(os.Getenv("MAILBOX_URL")),
		LLMAPIKey:       strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		WorkerCount:     workerCount,
		TaskTimeout:     taskTimeout,
		TaskMaxAttempts: maxAttempts,
		WorkerPoll:      workerPoll,
		TaskRetryBase:   retryBase,
	}, nil
}

func envPositiveInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
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
