package domain

import "time"

// SourceType names an adapter. New sources can be added without a closed database list.
type SourceType string

const (
	SourceGreenhouse SourceType = "greenhouse"
	SourceLever      SourceType = "lever"
	SourceEmail      SourceType = "email"
)

// SourceConfig holds non-secret source settings.
// A secret stays in the environment. SecretEnv is only the variable name.
type SourceConfig struct {
	BoardToken string `json:"board_token,omitempty"`
	Site       string `json:"site,omitempty"`
	SecretEnv  string `json:"secret_env,omitempty"`
}

// JobSource is one configured input, such as a Greenhouse board or a mailbox.
// Cursor is source-specific. For a mailbox it is the last provider message id.
// For a snapshot board it is the time of the last complete snapshot, not an API since parameter.
type JobSource struct {
	ID          string
	Name        string
	Type        SourceType
	Enabled     bool
	Config      SourceConfig
	Cursor      string
	LastChecked *time.Time
	LastSuccess *time.Time
}

// SourceReference is one time a source reported an opportunity.
// The first version does not merge two sources into one job.
type SourceReference struct {
	ID           string
	JobID        string
	SourceID     string
	ExternalID   *string
	OriginalURL  string
	CanonicalURL string
	DiscoveredAt time.Time
}
