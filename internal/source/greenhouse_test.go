package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGreenhouseFetchReadsDescriptions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jobs": [
				{
					"id": 127817,
					"title": "Systems Software Intern",
					"company_name": "Vault-Tec",
					"updated_at": "2016-01-14T10:55:28-05:00",
					"absolute_url": "https://boards.greenhouse.io/vaulttec/jobs/127817?utm_source=alert",
					"language": "en",
					"location": {"name": "NYC"},
					"content": "Build systems. &lt;p&gt;Go and Linux&lt;/p&gt;",
					"metadata": [{"name": "Employment Type", "value": "Intern", "value_type": "text"}],
					"offices": [
						{"id": 1, "name": "East Coast", "location": "United States", "child_ids": [2]},
						{"id": 2, "name": "New York City", "location": "New York, NY, United States", "child_ids": []}
					]
				},
				"not-a-job"
			],
			"meta": {"total": 2}
		}`))
	}))
	defer server.Close()

	snapshot, err := (Greenhouse{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "vault tec")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if gotPath != "/v1/boards/vault%20tec/jobs?content=true" {
		t.Fatalf("request = %s, want content=true on the jobs list", gotPath)
	}
	if !snapshot.Complete {
		t.Fatal("snapshot was not complete")
	}
	if len(snapshot.ItemErrors) != 1 || len(snapshot.Postings) != 1 {
		t.Fatalf("postings = %d errors = %d", len(snapshot.Postings), len(snapshot.ItemErrors))
	}
	job := snapshot.Postings[0]
	if job.ExternalID == nil || *job.ExternalID != "127817" {
		t.Fatalf("external id = %v", job.ExternalID)
	}
	if job.Completeness != "full" || !strings.Contains(job.Description, "Go and Linux") || strings.Contains(job.Description, "<p>") {
		t.Fatalf("description = %q", job.Description)
	}
	if job.Company == nil || *job.Company != "Vault-Tec" || job.Language == nil || *job.Language != "en" {
		t.Fatalf("company/language = %v %v", job.Company, job.Language)
	}
	if job.EmploymentType != "Intern" {
		t.Fatalf("employment = %q", job.EmploymentType)
	}
	if len(job.Locations) != 1 || job.Locations[0].Raw != "New York, NY, United States" {
		t.Fatalf("locations = %+v", job.Locations)
	}
	wantPosted := time.Date(2016, 1, 14, 15, 55, 28, 0, time.UTC)
	if job.PostedAt == nil || !job.PostedAt.Equal(wantPosted) {
		t.Fatalf("posted_at = %v", job.PostedAt)
	}
}

func TestGreenhouseFetchFailureIsNotASnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := (Greenhouse{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "vaulttec")
	if err == nil {
		t.Fatal("Fetch() error = nil, want the HTTP failure")
	}
}

func TestGreenhouseMissingDescriptionIsAnItemError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jobs":[{"id":7,"title":"Intern","absolute_url":"https://boards.example/jobs/7"}]}`))
	}))
	defer server.Close()

	snapshot, err := (Greenhouse{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "vaulttec")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(snapshot.Postings) != 0 || len(snapshot.ItemErrors) != 1 || snapshot.ItemErrors[0].ExternalID != "7" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
