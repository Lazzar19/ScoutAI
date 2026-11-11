package domain

import "time"


type TaskType string

const (
	TaskMatch TaskType = "match"
	TaskPoll  TaskType = "poll"
)


type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskProcessing TaskStatus = "processing"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
)


type Task struct {
	ID             string
	Type           TaskType
	JobID          *string
	SourceID       *string
	ContentHash    *string
	ProfileVersion *int
	Status         TaskStatus
	Attempts       int
	AvailableAt    time.Time
	LockedAt       *time.Time
	LockedBy       *string
	LastError      *string
}
