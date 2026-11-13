package domain

import "time"

// Feedback is one button press. It does not change match weights.
type Feedback struct {
	ID             string
	JobID          string
	ProfileID      string
	NotificationID string
	Type           FeedbackType
	CreatedAt      time.Time
}

type FeedbackType string

const (
	FeedbackInteresting FeedbackType = "interesting"
	FeedbackNotRelevant FeedbackType = "not_relevant"
	FeedbackSaved       FeedbackType = "saved_for_later"
	FeedbackApplied     FeedbackType = "already_applied"
)
