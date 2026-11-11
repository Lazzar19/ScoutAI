package domain

import "time"

// DecisionKind is a preview decision or a scored evaluation.
// The same two values are used for evaluation rows and notification types.
type DecisionKind string

const (
	DecisionPreview   DecisionKind = "preview"
	DecisionEvaluated DecisionKind = "evaluated"
)

// JobEvaluation is one stored decision for a job and a profile version.
// Rules produce the score. A preview decision leaves the score fields nil.
// EvaluationData holds extracted facts when an extractor result exists.
type JobEvaluation struct {
	ID             string
	JobID          string
	ProfileID      string
	ProfileVersion int
	ContentHash    string
	PromptVersion  string
	Model          string

	Kind            DecisionKind
	OverallScore    *int
	SkillsScore     *int
	RoleScore       *int
	ExperienceScore *int
	LocationScore   *int
	CompanyScore    *int

	EvaluationData []byte
	EvaluatedAt    time.Time
}
