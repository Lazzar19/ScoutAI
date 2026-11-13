package notify

import (
	"strings"
	"testing"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func TestPreviewTextHasNoScore(t *testing.T) {
	company := "Example Technologies"
	text := PreviewText(domain.Job{
		Title:     "Go_Intern *_*[v1]*",
		Company:   &company,
		Locations: []domain.Location{{Raw: "Europe", Remote: true}},
	}, "LinkedIn alerts", "https://www.linkedin.com/jobs/view/1?utm_source=alert")

	if strings.Contains(text, "/100") {
		t.Fatalf("preview showed a score:\n%s", text)
	}
	for _, want := range []string{
		"PREVIEW — description not in the source",
		"Go_Intern *_*[v1]*",
		"Company: Example Technologies",
		"Location: Remote / Europe",
		"Reply to this message with the text to score it.",
		"Source: LinkedIn alerts",
		"URL: https://www.linkedin.com/jobs/view/1?utm_source=alert",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("preview missing %q\n%s", want, text)
		}
	}
}

func TestEvaluatedTextShowsTheRuleScore(t *testing.T) {
	text := EvaluatedText(domain.Job{Title: "Backend Intern"}, 84, "Greenhouse", "https://boards.example/jobs/1")
	if !strings.Contains(text, "MATCH 84/100") || !strings.Contains(text, "Backend Intern") {
		t.Fatalf("evaluated text =\n%s", text)
	}
}

func TestFeedbackButtonsUseTheNotification(t *testing.T) {
	buttons := FeedbackButtons("job-note")
	if len(buttons) != 2 || len(buttons[0]) != 2 || len(buttons[1]) != 2 {
		t.Fatalf("buttons = %+v", buttons)
	}
	if buttons[0][0].Data != "fb:interesting:job-note" || buttons[1][1].Data != "fb:already_applied:job-note" {
		t.Fatalf("buttons = %+v", buttons)
	}
}
