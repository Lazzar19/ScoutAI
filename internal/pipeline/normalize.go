package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/source"
)

// normalize prepares one posting for storage.
// The title is only trimmed. Tracking parameters are removed from the canonical URL.
func normalize(posting source.DiscoveredPosting) (domain.Job, error) {
	title := strings.TrimSpace(posting.Title)
	if title == "" {
		return domain.Job{}, fmt.Errorf("title is empty")
	}
	canonical, err := canonicalURL(posting.CanonicalURL)
	if err != nil {
		return domain.Job{}, err
	}
	if canonical == "" {
		canonical, err = canonicalURL(posting.OriginalURL)
		if err != nil {
			return domain.Job{}, err
		}
	}
	externalID := posting.ExternalID
	if externalID != nil && strings.TrimSpace(*externalID) == "" {
		externalID = nil
	}
	if externalID == nil && canonical == "" {
		return domain.Job{}, fmt.Errorf("canonical url or external id is required")
	}

	var locations []domain.Location
	for _, location := range posting.Locations {
		raw := strings.Join(strings.Fields(location.Raw), " ")
		if raw == "" {
			continue
		}
		lower := strings.ToLower(raw)
		locations = append(locations, domain.Location{
			Raw:    raw,
			Remote: strings.Contains(lower, "remote") || location.Remote,
			Hybrid: strings.Contains(lower, "hybrid") || location.Hybrid,
		})
	}

	jobURL := canonical
	if jobURL == "" {
		jobURL = strings.TrimSpace(posting.OriginalURL)
	}
	if jobURL == "" {
		return domain.Job{}, fmt.Errorf("url is required")
	}
	company := cleanOptional(posting.Company)
	job := domain.Job{
		Kind:           posting.Kind,
		Completeness:   posting.Completeness,
		Title:          title,
		Company:        company,
		Locations:      locations,
		Description:    strings.Join(strings.Fields(posting.Description), " "),
		Language:       cleanOptional(posting.Language),
		URL:            jobURL,
		ApplyMethod:    domain.ApplyURL,
		EmploymentType: strings.Join(strings.Fields(posting.EmploymentType), " "),
		PostedAt:       posting.PostedAt,
		Status:         domain.JobOpen,
	}
	if job.Kind == "" {
		job.Kind = domain.KindListing
	}
	if job.Completeness == "" {
		job.Completeness = domain.CompletenessFull
	}
	hash, err := contentHash(job)
	if err != nil {
		return domain.Job{}, err
	}
	job.ContentHash = hash
	return job, nil
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.Join(strings.Fields(*value), " ")
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func canonicalURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("url must be absolute")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	query := parsed.Query()
	for key := range query {
		if trackingParam(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func trackingParam(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "gh_src", "fbclid", "gclid", "mc_eid", "mc_cid":
		return true
	default:
		return strings.HasPrefix(lower, "utm_")
	}
}

// ContentHash is the cache key for the normalized posting fields.
func ContentHash(job domain.Job) (string, error) {
	return contentHash(job)
}

func contentHash(job domain.Job) (string, error) {
	payload := struct {
		Title          string         `json:"title"`
		Company        string         `json:"company"`
		Locations      []hashLocation `json:"locations"`
		Description    string         `json:"description"`
		Language       string         `json:"language"`
		EmploymentType string         `json:"employment_type"`
		URL            string         `json:"url"`
	}{
		Title:          job.Title,
		Company:        deref(job.Company),
		Description:    job.Description,
		Language:       deref(job.Language),
		EmploymentType: job.EmploymentType,
		URL:            job.URL,
	}
	for _, location := range job.Locations {
		payload.Locations = append(payload.Locations, hashLocation{
			Raw:    location.Raw,
			Remote: location.Remote,
			Hybrid: location.Hybrid,
		})
	}
	if payload.Locations == nil {
		payload.Locations = []hashLocation{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("hash job: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type hashLocation struct {
	Raw    string `json:"raw"`
	Remote bool   `json:"remote"`
	Hybrid bool   `json:"hybrid"`
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
