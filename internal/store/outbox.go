package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// ListPendingNotifications returns outbox rows still waiting to be sent to one recipient.
func (p *Pool) ListPendingNotifications(ctx context.Context, recipient string, types []domain.DecisionKind, limit int) ([]domain.Notification, error) {
	if strings.TrimSpace(recipient) == "" || len(types) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	names := make([]string, len(types))
	for i, kind := range types {
		names[i] = string(kind)
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, job_id, evaluation_id, recipient, notification_type, channel, status,
		       external_message_id, sent_at, error
		FROM notifications
		WHERE status = 'pending'
		  AND channel = 'telegram'
		  AND recipient = $1
		  AND notification_type = ANY($2)
		ORDER BY created_at
		LIMIT $3`,
		recipient, names, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending notifications: %w", err)
	}
	defer rows.Close()
	var notifications []domain.Notification
	for rows.Next() {
		notification, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("list pending notifications: %w", err)
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending notifications: %w", err)
	}
	return notifications, nil
}

// MarkNotificationSent records the Telegram message id. A row that is no longer pending is left as it is.
func (p *Pool) MarkNotificationSent(ctx context.Context, id, messageID string, sentAt time.Time) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE notifications
		SET status = 'sent', external_message_id = $2, sent_at = $3, error = NULL
		WHERE id = $1::uuid AND status = 'pending'`,
		id, messageID, sentAt.UTC(),
	)
	return rowsOrNotFound("mark notification sent", tag, err)
}

// MarkNotificationRetry keeps the row pending and stores the latest delivery error.
func (p *Pool) MarkNotificationRetry(ctx context.Context, id, reason string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE notifications SET error = $2
		WHERE id = $1::uuid AND status = 'pending'`,
		id, clip(reason),
	)
	return rowsOrNotFound("mark notification retry", tag, err)
}

// MarkNotificationFailed stops delivery for one row and keeps the error.
func (p *Pool) MarkNotificationFailed(ctx context.Context, id, reason string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE notifications SET status = 'failed', error = $2
		WHERE id = $1::uuid AND status = 'pending'`,
		id, clip(reason),
	)
	return rowsOrNotFound("mark notification failed", tag, err)
}

// FindNotificationByMessage reads the outbox row for a Telegram message id.
func (p *Pool) FindNotificationByMessage(ctx context.Context, messageID string) (domain.Notification, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, evaluation_id, recipient, notification_type, channel, status,
		       external_message_id, sent_at, error
		FROM notifications
		WHERE external_message_id = $1
		ORDER BY sent_at DESC NULLS LAST
		LIMIT 1`, messageID)
	notification, err := scanNotification(row)
	if err != nil {
		return domain.Notification{}, mapReadErr("find notification by message", err)
	}
	return notification, nil
}

// JobOrigin returns the source name and the original URL for a job.
func (p *Pool) JobOrigin(ctx context.Context, jobID string) (string, string, error) {
	var name, original string
	err := p.pool.QueryRow(ctx, `
		SELECT s.name, r.original_url
		FROM job_source_references r
		JOIN sources s ON s.id = r.source_id
		WHERE r.job_id = $1::uuid
		ORDER BY r.discovered_at
		LIMIT 1`, jobID).Scan(&name, &original)
	if err != nil {
		return "", "", mapReadErr("job origin", err)
	}
	return name, original, nil
}

// UpgradeJobText stores pasted posting text and marks the job full.
// The URL and the other posting fields stay as they were.
func (p *Pool) UpgradeJobText(ctx context.Context, jobID, description, contentHash string) error {
	description = strings.Join(strings.Fields(description), " ")
	if description == "" || strings.TrimSpace(contentHash) == "" {
		return fmt.Errorf("upgrade job text: description and content hash are required")
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE jobs
		SET description = $2, completeness = 'full', content_hash = $3, updated_at = now()
		WHERE id = $1::uuid`,
		jobID, description, contentHash,
	)
	return rowsOrNotFound("upgrade job text", tag, err)
}

// InsertFeedback stores a button press. The same choice for one notification is not stored twice.
// Profile preferences and match weights are not updated.
func (p *Pool) InsertFeedback(ctx context.Context, jobID, profileID, notificationID string, kind domain.FeedbackType) error {
	if !validFeedback(kind) {
		return fmt.Errorf("insert feedback: unknown feedback type %s", kind)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("insert feedback: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var current string
	err = tx.QueryRow(ctx, `
		SELECT feedback_type
		FROM user_feedback
		WHERE notification_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, notificationID).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("insert feedback: %w", err)
	}
	if current == string(kind) {
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO user_feedback (id, job_id, profile_id, notification_id, feedback_type)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5)`,
		domain.NewID(), jobID, profileID, notificationID, string(kind),
	)
	if err != nil {
		return mapWriteErr("insert feedback", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("insert feedback: commit: %w", err)
	}
	return nil
}

// ListFeedback returns the choices stored for one notification, oldest first.
func (p *Pool) ListFeedback(ctx context.Context, notificationID string) ([]domain.Feedback, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, job_id, profile_id, notification_id, feedback_type, created_at
		FROM user_feedback
		WHERE notification_id = $1::uuid
		ORDER BY created_at, id`, notificationID)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	defer rows.Close()
	var choices []domain.Feedback
	for rows.Next() {
		var choice domain.Feedback
		var kind string
		if err := rows.Scan(&choice.ID, &choice.JobID, &choice.ProfileID, &choice.NotificationID, &kind, &choice.CreatedAt); err != nil {
			return nil, fmt.Errorf("list feedback: %w", err)
		}
		choice.Type = domain.FeedbackType(kind)
		choice.CreatedAt = choice.CreatedAt.UTC()
		choices = append(choices, choice)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	return choices, nil
}

// NotifyStatus counts open jobs and pending outbox rows.
func (p *Pool) NotifyStatus(ctx context.Context) (int, int, error) {
	var openJobs, pending int
	err := p.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM jobs WHERE status = 'open'),
			(SELECT count(*) FROM notifications WHERE status = 'pending')`).Scan(&openJobs, &pending)
	if err != nil {
		return 0, 0, fmt.Errorf("notify status: %w", err)
	}
	return openJobs, pending, nil
}

func validFeedback(kind domain.FeedbackType) bool {
	switch kind {
	case domain.FeedbackInteresting, domain.FeedbackNotRelevant, domain.FeedbackSaved, domain.FeedbackApplied:
		return true
	default:
		return false
	}
}

func clip(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
