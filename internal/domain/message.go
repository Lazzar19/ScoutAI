package domain

import "time"

// RawMessage is one ingested email.
// The body stays in the database so a parser change can replay it. Do not log RawBody.
type RawMessage struct {
	ID                string
	SourceID          string
	ProviderMessageID string
	ReceivedAt        time.Time
	ParseStatus       string
	ParseError        string
	RawBody           string
	CreatedAt         time.Time
}
