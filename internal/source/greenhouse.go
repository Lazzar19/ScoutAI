package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

const (
	defaultGreenhouseBase = "https://boards-api.greenhouse.io"
	greenhouseTimeout     = 30 * time.Second
)

// Greenhouse calls the public job-board API. Board tokens are not secrets.
type Greenhouse struct {
	BaseURL string
	Client  *http.Client
}

// Board binds one token to the snapshot interface.
type Board struct {
	Greenhouse Greenhouse
	Token      string
	Label      string
}

func (b Board) Name() string {
	if b.Label != "" {
		return b.Label
	}
	return b.Token
}

func (b Board) Fetch(ctx context.Context) (Snapshot, error) {
	return b.Greenhouse.Fetch(ctx, b.Token)
}

// Fetch reads one board with descriptions included.
// The list endpoint returns the whole board, so a successful parse is a complete snapshot.
// A transport or HTTP failure returns an error and is not a partial snapshot.
func (g Greenhouse) Fetch(ctx context.Context, token string) (Snapshot, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Snapshot{}, fmt.Errorf("greenhouse: board token is required")
	}
	ctx, cancel := context.WithTimeout(ctx, greenhouseTimeout)
	defer cancel()

	endpoint, err := g.jobsURL(token)
	if err != nil {
		return Snapshot{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("greenhouse: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ScoutAI")

	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("greenhouse: request board: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Snapshot{}, fmt.Errorf("greenhouse: read board: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("greenhouse: board returned %s", resp.Status)
	}

	var payload struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Snapshot{}, fmt.Errorf("greenhouse: decode board: %w", err)
	}
	if payload.Jobs == nil {
		return Snapshot{}, fmt.Errorf("greenhouse: jobs list is missing")
	}

	snapshot := Snapshot{Complete: true}
	for _, raw := range payload.Jobs {
		posting, itemErr := decodeGreenhouseJob(raw)
		if itemErr != nil {
			snapshot.ItemErrors = append(snapshot.ItemErrors, *itemErr)
			continue
		}
		snapshot.Postings = append(snapshot.Postings, posting)
	}
	return snapshot, nil
}

func (g Greenhouse) jobsURL(token string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(g.BaseURL), "/")
	if base == "" {
		base = defaultGreenhouseBase
	}
	joined, err := url.JoinPath(base, "v1", "boards", token, "jobs")
	if err != nil {
		return "", fmt.Errorf("greenhouse: build board url: %w", err)
	}
	u, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("greenhouse: parse board url: %w", err)
	}
	query := u.Query()
	query.Set("content", "true")
	u.RawQuery = query.Encode()
	return u.String(), nil
}

type greenhouseJob struct {
	ID             int64              `json:"id"`
	Title          string             `json:"title"`
	CompanyName    string             `json:"company_name"`
	UpdatedAt      string             `json:"updated_at"`
	FirstPublished string             `json:"first_published"`
	AbsoluteURL    string             `json:"absolute_url"`
	Language       string             `json:"language"`
	Content        *string            `json:"content"`
	Location       greenhousePlace    `json:"location"`
	Offices        []greenhouseOffice `json:"offices"`
	Metadata       json.RawMessage    `json:"metadata"`
}

type greenhousePlace struct {
	Name string `json:"name"`
}

type greenhouseOffice struct {
	Name     string  `json:"name"`
	Location string  `json:"location"`
	ChildIDs []int64 `json:"child_ids"`
}

func decodeGreenhouseJob(raw json.RawMessage) (DiscoveredPosting, *ItemError) {
	var job greenhouseJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return DiscoveredPosting{}, &ItemError{Message: "job could not be decoded"}
	}
	externalID := ""
	if job.ID != 0 {
		externalID = strconv.FormatInt(job.ID, 10)
	}
	if job.Content == nil {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "description was not in the board response"}
	}
	posting := DiscoveredPosting{
		Kind:           domain.KindListing,
		Completeness:   domain.CompletenessFull,
		Title:          job.Title,
		OriginalURL:    job.AbsoluteURL,
		CanonicalURL:   job.AbsoluteURL,
		Description:    htmlText(*job.Content),
		Locations:      greenhouseLocations(job),
		EmploymentType: greenhouseEmployment(job.Metadata),
	}
	if externalID != "" {
		posting.ExternalID = &externalID
	}
	if company := collapseSpace(job.CompanyName); company != "" {
		posting.Company = &company
	}
	if lang := strings.TrimSpace(job.Language); lang != "" {
		posting.Language = &lang
	}
	if posted := greenhouseTime(job.FirstPublished, job.UpdatedAt); posted != nil {
		posting.PostedAt = posted
	}
	return posting, nil
}

func greenhouseLocations(job greenhouseJob) []domain.Location {
	var leaves []greenhouseOffice
	for _, office := range job.Offices {
		if len(office.ChildIDs) == 0 {
			leaves = append(leaves, office)
		}
	}
	if len(leaves) == 0 {
		leaves = job.Offices
	}
	var locations []domain.Location
	for _, office := range leaves {
		raw := collapseSpace(office.Location)
		if raw == "" {
			raw = collapseSpace(office.Name)
		}
		if raw == "" {
			continue
		}
		locations = append(locations, locationFromRaw(raw))
	}
	if len(locations) == 0 {
		if raw := collapseSpace(job.Location.Name); raw != "" {
			locations = append(locations, locationFromRaw(raw))
		}
	}
	return locations
}

func locationFromRaw(raw string) domain.Location {
	lower := strings.ToLower(raw)
	return domain.Location{
		Raw:    raw,
		Remote: strings.Contains(lower, "remote"),
		Hybrid: strings.Contains(lower, "hybrid"),
	}
}

func greenhouseEmployment(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var fields []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	for _, field := range fields {
		if !strings.Contains(strings.ToLower(field.Name), "employment") {
			continue
		}
		var value string
		if err := json.Unmarshal(field.Value, &value); err != nil {
			continue
		}
		return collapseSpace(value)
	}
	return ""
}

func greenhouseTime(published, updated string) *time.Time {
	for _, value := range []string{published, updated} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		utc := parsed.UTC()
		return &utc
	}
	return nil
}
