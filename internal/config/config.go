// Package config loads process settings from the environment.
package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// Config is the process configuration. Only DatabaseURL is required.
// Telegram, mailbox, and model credentials stay empty until those features are enabled.
type Config struct {
	DatabaseURL   string
	HTTPAddr      string
	TelegramToken string
	MailboxURL    string
	LLMAPIKey     string
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

	return Config{
		DatabaseURL:   databaseURL,
		HTTPAddr:      net.JoinHostPort("0.0.0.0", port),
		TelegramToken: strings.TrimSpace(os.Getenv("TELEGRAM_TOKEN")),
		MailboxURL:    strings.TrimSpace(os.Getenv("MAILBOX_URL")),
		LLMAPIKey:     strings.TrimSpace(os.Getenv("LLM_API_KEY")),
	}, nil
}
