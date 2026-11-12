package source

import (
	"context"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// SnapshotSource reads one board into discovered postings.
// Fetch does not persist anything. Complete is true only when every page was read.
type SnapshotSource interface {
	Name() string
	Fetch(ctx context.Context) (Snapshot, error)
}

// Snapshot is one poll of a board.
// ItemErrors are postings that could not be read. They do not discard the rest of the snapshot.
type Snapshot struct {
	Postings   []DiscoveredPosting
	Complete   bool
	ItemErrors []ItemError
}

// DiscoveredPosting is one opportunity before normalization and storage.
type DiscoveredPosting struct {
	ExternalID     *string
	OriginalURL    string
	CanonicalURL   string
	Kind           domain.OpportunityKind
	Completeness   domain.Completeness
	Title          string
	Company        *string
	Locations      []domain.Location
	Description    string
	Language       *string
	PostedAt       *time.Time
	EmploymentType string
}

// ItemError is one posting the adapter could not read.
type ItemError struct {
	ExternalID string
	Message    string
}

func (e ItemError) Error() string {
	if e.ExternalID == "" {
		return e.Message
	}
	return e.ExternalID + ": " + e.Message
}
