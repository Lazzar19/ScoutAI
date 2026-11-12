package match

import (
	"testing"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func TestPreviewMatchHasNoScore(t *testing.T) {
	job := domain.Job{
		Completeness: domain.CompletenessPreview,
		Title:        "Systems Software Intern",
		Company:      strPtr("Example"),
	}
	outcome := Decide(job, studentProfile(), DefaultRules())
	if outcome.Kind != domain.DecisionPreview || !outcome.Notify {
		t.Fatalf("outcome = %+v", outcome)
	}
	requireNilScores(t, outcome)
}

func TestPreviewRejectsUnrelatedExcludedAndDistantJobs(t *testing.T) {
	profile := studentProfile()
	profile.ExcludedRoles = []string{"sales"}
	rules := DefaultRules()

	unrelated := Decide(domain.Job{Completeness: domain.CompletenessPreview, Title: "Account Executive"}, profile, rules)
	if unrelated.Notify {
		t.Fatal("an unrelated title notified")
	}
	internal := Decide(domain.Job{Completeness: domain.CompletenessPreview, Title: "Internal Tools Engineer"}, profile, rules)
	if internal.Notify {
		t.Fatal("internal matched the intern synonym")
	}
	excludedRole := Decide(domain.Job{Completeness: domain.CompletenessPreview, Title: "Sales Intern"}, profile, rules)
	if excludedRole.Notify {
		t.Fatal("an excluded role notified")
	}
	excludedCompany := Decide(domain.Job{
		Completeness: domain.CompletenessPreview,
		Title:        "Backend Intern",
		Company:      strPtr("Excluded Co"),
	}, profile, rules)
	if excludedCompany.Notify {
		t.Fatal("an excluded company notified")
	}
	distant := Decide(domain.Job{
		Completeness: domain.CompletenessPreview,
		Title:        "Backend Intern",
		Locations:    []domain.Location{{Raw: "New York, United States"}},
	}, profile, rules)
	if distant.Notify {
		t.Fatal("a distant location notified")
	}
	for _, outcome := range []Outcome{unrelated, internal, excludedRole, excludedCompany, distant} {
		requireNilScores(t, outcome)
	}
}

func TestSerbianSynonymMatchesTheTitle(t *testing.T) {
	profile := studentProfile()
	rules := DefaultRules()
	latin := Decide(domain.Job{Completeness: domain.CompletenessPreview, Title: "Staž: Backend"}, profile, rules)
	cyrillic := Decide(domain.Job{Completeness: domain.CompletenessPreview, Title: "Пракса за студенте"}, profile, rules)
	if !latin.Notify || !cyrillic.Notify {
		t.Fatalf("latin = %v cyrillic = %v", latin.Notify, cyrillic.Notify)
	}
	requireNilScores(t, latin)
	requireNilScores(t, cyrillic)
}

func TestFullDescriptionReceivesARuleScore(t *testing.T) {
	job := domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Intern",
		Company:      strPtr("Example"),
		Locations:    []domain.Location{{Raw: "Remote", Remote: true}},
		Description:  "Build services in Go.",
	}
	outcome := Decide(job, studentProfile(), DefaultRules())
	if outcome.Kind != domain.DecisionEvaluated || !outcome.Notify {
		t.Fatalf("outcome = %+v", outcome)
	}
	requireInt(t, "role", outcome.Role, 30)
	requireInt(t, "skills", outcome.Skills, 15)
	requireInt(t, "experience", outcome.Experience, 20)
	requireInt(t, "location", outcome.Location, 10)
	requireInt(t, "company", outcome.Company, 10)
	requireInt(t, "overall", outcome.Overall, 85)
}

func TestUnknownFieldsDoNotZeroACriterion(t *testing.T) {
	job := domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Engineer",
		Description:  "Build software with the team. 2 years of experience preferred.",
	}
	outcome := Decide(job, studentProfile(), DefaultRules())
	requireInt(t, "role", outcome.Role, 30)
	requireInt(t, "overall", outcome.Overall, 100)
	requireNilPtr(t, "skills", outcome.Skills)
	requireNilPtr(t, "experience", outcome.Experience)
	requireNilPtr(t, "location", outcome.Location)
	requireNilPtr(t, "company", outcome.Company)
	if !outcome.Notify {
		t.Fatal("unknown fields kept the job under the threshold")
	}
}

func TestExcludedStudentsZeroExperience(t *testing.T) {
	job := domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Engineer",
		Description:  "Students will not be considered. 2 years of experience required.",
	}
	outcome := Decide(job, studentProfile(), DefaultRules())
	requireInt(t, "experience", outcome.Experience, 0)
	requireInt(t, "overall", outcome.Overall, 60)
}

func TestWeightsChangeTheScore(t *testing.T) {
	job := domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Backend Engineer",
		Company:      strPtr("Excluded Co"),
		Description:  "Build software with the team.",
	}
	profile := studentProfile()
	light := DefaultRules()
	heavy := DefaultRules()
	heavy.Weights.Role = 10
	heavy.Weights.Company = 30

	first := Decide(job, profile, light)
	second := Decide(job, profile, heavy)
	requireInt(t, "light", first.Overall, 75)
	requireInt(t, "heavy", second.Overall, 25)
	if !first.Notify || second.Notify {
		t.Fatalf("notify light = %v heavy = %v", first.Notify, second.Notify)
	}
}

func studentProfile() domain.CandidateProfile {
	return domain.CandidateProfile{
		RoleSynonyms:       []string{"intern", "internship", "praksa", "staž"},
		PreferredRoles:     []string{"backend engineer"},
		PreferredSkills:    []string{"Go", "Linux"},
		PreferredLocations: []domain.Location{{Raw: "Serbia"}, {Raw: "Remote", Remote: true}},
		PreferredCompanies: []string{"Example"},
		ExcludedCompanies:  []string{"Excluded Co"},
		ExperienceLevel:    "student",
	}
}

func strPtr(value string) *string {
	return &value
}

func requireNilScores(t *testing.T, outcome Outcome) {
	t.Helper()
	requireNilPtr(t, "overall", outcome.Overall)
	requireNilPtr(t, "role", outcome.Role)
	requireNilPtr(t, "skills", outcome.Skills)
	requireNilPtr(t, "experience", outcome.Experience)
	requireNilPtr(t, "location", outcome.Location)
	requireNilPtr(t, "company", outcome.Company)
}

func requireInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %d", name, want)
	}
	if *got != want {
		t.Fatalf("%s = %d, want %d", name, *got, want)
	}
}

func requireNilPtr(t *testing.T, name string, got *int) {
	t.Helper()
	if got != nil {
		t.Fatalf("%s = %d, want nil", name, *got)
	}
}
