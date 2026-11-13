package source

import (
	_ "embed"
	"net/http"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/alert.eml
var alertFixture []byte

func TestParseLinkedInAlertReadsEveryListing(t *testing.T) {
	transport := &rejectTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	postings, err := ParseLinkedInAlert(alertFixture)
	if err != nil {
		t.Fatalf("ParseLinkedInAlert() error = %v", err)
	}
	if transport.calls != 0 {
		t.Fatalf("parser made %d HTTP requests", transport.calls)
	}
	if len(postings) != 2 {
		t.Fatalf("listings = %d, want 2", len(postings))
	}
	wantPosted := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	first := postings[0]
	if first.Title != "Backend Intern" || first.Company == nil || *first.Company != "Acme" {
		t.Fatalf("first = %+v", first)
	}
	if first.Description != "" || strings.Contains(first.Description, "Build services in Go.") {
		t.Fatalf("description = %q", first.Description)
	}
	if first.Completeness != "preview" || first.Kind != "listing" {
		t.Fatalf("kind = %s completeness = %s", first.Kind, first.Completeness)
	}
	if first.ExternalID == nil || *first.ExternalID != "3988776655" {
		t.Fatalf("external id = %v", first.ExternalID)
	}
	if first.CanonicalURL != "https://www.linkedin.com/jobs/view/3988776655" {
		t.Fatalf("canonical = %s", first.CanonicalURL)
	}
	if !strings.Contains(first.OriginalURL, "/comm/jobs/view/3988776655") || !strings.Contains(first.OriginalURL, "trk=") {
		t.Fatalf("original = %s", first.OriginalURL)
	}
	if len(first.Locations) != 1 || first.Locations[0].Raw != "Belgrade, Serbia (Hybrid)" {
		t.Fatalf("location = %+v", first.Locations)
	}
	if first.PostedAt == nil || !first.PostedAt.Equal(wantPosted) {
		t.Fatalf("posted = %v", first.PostedAt)
	}
	second := postings[1]
	if second.Title != "Platform Intern" || second.Company == nil || *second.Company != "Northwind" {
		t.Fatalf("second = %+v", second)
	}
	if len(second.Locations) != 1 || second.Locations[0].Raw != "Remote" {
		t.Fatalf("second location = %+v", second.Locations)
	}
	if second.CanonicalURL != "https://www.linkedin.com/jobs/view/3988777001" {
		t.Fatalf("second canonical = %s", second.CanonicalURL)
	}
}

func TestParsePlainAlertKeepsTheOriginalQuery(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>",
		"Subject: 1 new job",
		"Date: Fri, 13 Nov 2026 09:00:00 +0000",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"Backend Intern",
		"Acme",
		"Belgrade, Serbia (Hybrid)",
		"View job: https://www.linkedin.com/comm/jobs/view/3988776655?trk=eml&trackingId=BBB",
	}, "\n"))
	postings, err := ParseLinkedInAlert(raw)
	if err != nil {
		t.Fatalf("ParseLinkedInAlert() error = %v", err)
	}
	if len(postings) != 1 || postings[0].Title != "Backend Intern" || postings[0].Description != "" {
		t.Fatalf("postings = %+v", postings)
	}
	if postings[0].CanonicalURL != "https://www.linkedin.com/jobs/view/3988776655" {
		t.Fatalf("canonical = %s", postings[0].CanonicalURL)
	}
	if !strings.Contains(postings[0].OriginalURL, "trk=eml") {
		t.Fatalf("original = %s", postings[0].OriginalURL)
	}
}

func TestParseLinkedInAlertReadsHTMLWhenPlainTextIsMissing(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>",
		"Subject: 1 new job",
		"Date: Fri, 13 Nov 2026 09:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		"<html><body>",
		`<a href="https://www.linkedin.com/comm/jobs/view/3988776655?trk=eml&amp;trackingId=BBB">Backend Intern</a>`,
		"<p>Acme · Belgrade, Serbia (Hybrid)</p>",
		"<p>Build services in Go.</p>",
		"</body></html>",
	}, "\n"))
	postings, err := ParseLinkedInAlert(raw)
	if err != nil {
		t.Fatalf("ParseLinkedInAlert() error = %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("listings = %d, want 1", len(postings))
	}
	got := postings[0]
	if got.Title != "Backend Intern" || got.Description != "" {
		t.Fatalf("posting = %+v", got)
	}
	if got.Company == nil || *got.Company != "Acme" {
		t.Fatalf("company = %v", got.Company)
	}
	if len(got.Locations) != 1 || got.Locations[0].Raw != "Belgrade, Serbia (Hybrid)" {
		t.Fatalf("location = %+v", got.Locations)
	}
	if strings.Contains(got.Locations[0].Raw, "Build services") {
		t.Fatalf("snippet was stored as the location: %s", got.Locations[0].Raw)
	}
	if got.CanonicalURL != "https://www.linkedin.com/jobs/view/3988776655" {
		t.Fatalf("canonical = %s", got.CanonicalURL)
	}
}

func TestParseLinkedInAlertRejectsAListingWithoutATitle(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>",
		"Subject: alert",
		"Date: Fri, 13 Nov 2026 09:05:00 +0000",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"View job: https://www.linkedin.com/comm/jobs/view/111222333?trk=eml",
	}, "\n"))
	postings, err := ParseLinkedInAlert(raw)
	if err == nil {
		t.Fatal("ParseLinkedInAlert() error = nil, want a parser error")
	}
	if len(postings) != 0 {
		t.Fatalf("listings = %d, want none", len(postings))
	}
	if strings.Contains(err.Error(), "111222333") || strings.Contains(err.Error(), "trk=") {
		t.Fatalf("parser error includes the message: %v", err)
	}
}

func TestParseLinkedInAlertRejectsAnUnreadableMessage(t *testing.T) {
	_, err := ParseLinkedInAlert([]byte("this is not an email\n\nbody"))
	if err == nil {
		t.Fatal("ParseLinkedInAlert() error = nil, want a read error")
	}
}

type rejectTransport struct {
	calls int
}

func (t *rejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, http.ErrNotSupported
}
