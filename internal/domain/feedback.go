package domain


type FeedbackType string

const (
	FeedbackInteresting FeedbackType = "interesting"
	FeedbackNotRelevant FeedbackType = "not_relevant"
	FeedbackSaved       FeedbackType = "saved_for_later"
	FeedbackApplied     FeedbackType = "already_applied"
)
