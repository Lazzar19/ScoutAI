package notify

import (
	"fmt"
	"strings"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// PreviewText is the plain-text preview. It has no score.
func PreviewText(job domain.Job, sourceName, originalURL string) string {
	return strings.Join([]string{
		"PREVIEW — description not in the source",
		"",
		job.Title,
		"Company: " + companyLine(job),
		"Location: " + locationLine(job.Locations),
		"",
		"Open the link to read the posting.",
		"Reply to this message with the text to score it.",
		"",
		"Source: " + orUnknown(sourceName),
		"URL: " + originalURL,
	}, "\n")
}

// EvaluatedText is the plain-text scored message. The number is the rule score.
func EvaluatedText(job domain.Job, score int, sourceName, originalURL string) string {
	return strings.Join([]string{
		fmt.Sprintf("MATCH %d/100", score),
		"",
		job.Title,
		"Company: " + companyLine(job),
		"Location: " + locationLine(job.Locations),
		"",
		"Source: " + orUnknown(sourceName),
		"URL: " + originalURL,
	}, "\n")
}

// FeedbackButtons are the four choices stored as feedback. They do not change weights.
func FeedbackButtons(notificationID string) [][]Button {
	return [][]Button{
		{
			{Text: "Interesting", Data: "fb:interesting:" + notificationID},
			{Text: "Not relevant", Data: "fb:not_relevant:" + notificationID},
		},
		{
			{Text: "Save for later", Data: "fb:saved_for_later:" + notificationID},
			{Text: "Already applied", Data: "fb:already_applied:" + notificationID},
		},
	}
}

func companyLine(job domain.Job) string {
	if job.Company == nil {
		return "Unknown"
	}
	company := strings.TrimSpace(*job.Company)
	if company == "" {
		return "Unknown"
	}
	return company
}

func locationLine(locations []domain.Location) string {
	var parts []string
	for _, location := range locations {
		if line := formatLocation(location); line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	return strings.Join(parts, "; ")
}

func formatLocation(location domain.Location) string {
	raw := strings.TrimSpace(location.Raw)
	if raw == "" && location.City != nil {
		raw = strings.TrimSpace(*location.City)
	}
	if raw == "" && location.Country != nil {
		raw = strings.TrimSpace(*location.Country)
	}
	if raw == "" && location.Remote {
		return "Remote"
	}
	if raw == "" {
		return ""
	}
	if location.Remote && !strings.Contains(strings.ToLower(raw), "remote") {
		return "Remote / " + raw
	}
	return raw
}

func orUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}

func profileText(profile domain.CandidateProfile) string {
	return strings.Join([]string{
		fmt.Sprintf("Profile: %s (version %d)", profile.Name, profile.Version),
		"",
		"Roles: " + listOrNone(profile.PreferredRoles),
		"Synonyms: " + listOrNone(profile.RoleSynonyms),
		"Skills: " + listOrNone(append(append([]string{}, profile.PreferredSkills...), profile.AdditionalSkills...)),
		"Locations: " + locationLine(profile.PreferredLocations),
		"Preferred companies: " + listOrNone(profile.PreferredCompanies),
		"Excluded companies: " + listOrNone(profile.ExcludedCompanies),
	}, "\n")
}

func statusText(openJobs, pending int, profile *domain.CandidateProfile) string {
	name := "none"
	if profile != nil {
		name = fmt.Sprintf("%s (version %d)", profile.Name, profile.Version)
	}
	return fmt.Sprintf("Open jobs: %d\nPending notifications: %d\nProfile: %s", openJobs, pending, name)
}

func sourcesText(sources []domain.JobSource) string {
	if len(sources) == 0 {
		return "No sources are configured."
	}
	lines := make([]string, 0, len(sources)*2)
	for _, source := range sources {
		state := "disabled"
		if source.Enabled {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("%s — %s, %s", source.Name, source.Type, state))
		if source.LastChecked == nil {
			lines = append(lines, "last check: never")
			continue
		}
		lines = append(lines, "last check: "+source.LastChecked.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return strings.Join(lines, "\n")
}

func listOrNone(values []string) string {
	var cleaned []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	if len(cleaned) == 0 {
		return "none"
	}
	return strings.Join(cleaned, ", ")
}

const helpText = `ScoutAI watches for internships and sends a preview or a scored match.

/profile shows the active profile.
/status shows the queue.
/sources lists boards.
Reply to a preview with the posting text to score it.
Use the buttons on a message to save feedback.`
