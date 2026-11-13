package pipeline

import "testing"

func TestCanonicalURLStripsTracking(t *testing.T) {
	got, err := canonicalURL("https://Boards.Greenhouse.io/vaulttec/jobs/127817/?utm_source=alert&gh_src=email")
	if err != nil {
		t.Fatalf("canonicalURL() error = %v", err)
	}
	want := "https://boards.greenhouse.io/vaulttec/jobs/127817"
	if got != want {
		t.Fatalf("canonicalURL() = %s, want %s", got, want)
	}

	got, err = canonicalURL("https://www.linkedin.com/comm/jobs/view/3988776655?trk=eml&trackingId=ABC&refId=DEF&utm_source=alert")
	if err != nil {
		t.Fatalf("canonicalURL() error = %v", err)
	}
	want = "https://www.linkedin.com/comm/jobs/view/3988776655"
	if got != want {
		t.Fatalf("canonicalURL() = %s, want %s", got, want)
	}
}
