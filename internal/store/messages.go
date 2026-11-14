package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertRawMessage stores one email under its provider message id.
// The same source and provider id cannot be stored twice.
func (p *Pool) InsertRawMessage(ctx context.Context, msg *domain.RawMessage) error {
	if msg == nil {
		return fmt.Errorf("insert raw message: message is required")
	}
	if strings.TrimSpace(msg.SourceID) == "" || strings.TrimSpace(msg.ProviderMessageID) == "" {
		return fmt.Errorf("insert raw message: source and provider id are required")
	}
	if strings.TrimSpace(msg.ParseStatus) == "" {
		return fmt.Errorf("insert raw message: parse status is required")
	}
	if msg.RawBody == "" {
		return fmt.Errorf("insert raw message: body is required")
	}
	if msg.ReceivedAt.IsZero() {
		return fmt.Errorf("insert raw message: received time is required")
	}
	ensureID(&msg.ID)
	var parseError any
	if strings.TrimSpace(msg.ParseError) != "" {
		parseError = msg.ParseError
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO raw_messages (
			id, source_id, provider_message_id, received_at, parse_status, parse_error, raw_body
		) VALUES (
			$1::uuid, $2::uuid, $3, $4, $5, $6, $7
		)`,
		msg.ID,
		msg.SourceID,
		msg.ProviderMessageID,
		msg.ReceivedAt.UTC(),
		msg.ParseStatus,
		parseError,
		msg.RawBody,
	)
	return mapWriteErr("insert raw message", err)
}

// FindRawMessage returns the stored email for one source and provider message id.
func (p *Pool) FindRawMessage(ctx context.Context, sourceID, providerMessageID string) (domain.RawMessage, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, source_id, provider_message_id, received_at, parse_status, parse_error, raw_body, created_at
		FROM raw_messages
		WHERE source_id = $1::uuid AND provider_message_id = $2`,
		sourceID, providerMessageID,
	)
	msg, err := scanRawMessage(row)
	if err != nil {
		return domain.RawMessage{}, mapReadErr("find raw message", err)
	}
	return msg, nil
}

func scanRawMessage(row rowScanner) (domain.RawMessage, error) {
	var msg domain.RawMessage
	var parseError *string
	err := row.Scan(
		&msg.ID,
		&msg.SourceID,
		&msg.ProviderMessageID,
		&msg.ReceivedAt,
		&msg.ParseStatus,
		&parseError,
		&msg.RawBody,
		&msg.CreatedAt,
	)
	if err != nil {
		return domain.RawMessage{}, err
	}
	if parseError != nil {
		msg.ParseError = *parseError
	}
	msg.ReceivedAt = msg.ReceivedAt.UTC()
	msg.CreatedAt = msg.CreatedAt.UTC()
	return msg, nil
}

// SetSourceCursor records the last provider message id whose email was stored.
func (p *Pool) SetSourceCursor(ctx context.Context, sourceID, cursor string, at time.Time) error {
	if strings.TrimSpace(cursor) == "" {
		return fmt.Errorf("set source cursor: cursor is empty")
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE sources
		SET cursor = $2, last_success_at = $3
		WHERE id = $1::uuid`,
		sourceID, cursor, at.UTC(),
	)
	return rowsOrNotFound("set source cursor", tag, err)
}
