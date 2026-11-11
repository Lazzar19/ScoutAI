package domain

import "time"

// OpportunityKind distinguishes a structured listing from a user-submitted post.
type OpportunityKind string

const (
	KindListing OpportunityKind = "listing"
	KindPost    OpportunityKind = "post"
)

// Completeness says whether the description is actually present.
type Completeness string

const (
	CompletenessPreview Completeness = "preview"
	CompletenessFull    Completeness = "full"
)

// ApplyMethod is how a person reaches the opportunity.
type ApplyMethod string

const (
	ApplyURL           ApplyMethod = "url"
	ApplyContactAuthor ApplyMethod = "contact_author"
	ApplyUnknown       ApplyMethod = "unknown"
)

// JobStatus is the listing lifecycle. It is separate from queue state.
type JobStatus string

const (
	JobOpen    JobStatus = "open"
	JobClosed  JobStatus = "closed"
	JobUnknown JobStatus = "unknown"
)

// Location is one place attached to an opportunity or a profile.
// Nil city or country means that part was not in the source.
type Location struct {
	City    *string `json:"city,omitempty"`
	Country *string `json:"country,omitempty"`
	Remote  bool    `json:"remote"`
	Hybrid  bool    `json:"hybrid"`
	Raw     string  `json:"raw,omitempty"`
}

// Job is one normalized opportunity.
// ContentHash covers the fields that should trigger a new evaluation when they change. It is not a uniqueness key.
type Job struct {
	ID           string
	Kind         OpportunityKind
	Completeness Completeness

	Title       string
	Company     *string
	Author      *string
	Locations   []Location
	Description string
	Language    *string

	URL            string
	ApplyMethod    ApplyMethod
	EmploymentType string
	Experience     string
	Deadline       *time.Time

	PostedAt     *time.Time
	DiscoveredAt time.Time

	ContentHash string
	Status      JobStatus
}
