package domain

import "time"


type DeliveryStatus string

const (
	DeliveryPending DeliveryStatus = "pending"
	DeliverySent    DeliveryStatus = "sent"
	DeliveryFailed  DeliveryStatus = "failed"
)


type NotificationChannel string

const (
	ChannelTelegram NotificationChannel = "telegram"
)



type Notification struct {
	ID                string
	JobID             string
	EvaluationID      *string
	Recipient         string
	Type              DecisionKind
	Channel           NotificationChannel
	Status            DeliveryStatus
	ExternalMessageID *string
	SentAt            *time.Time
	Error             *string
}
