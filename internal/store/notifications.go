package store

import (
	"context"
	"fmt"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertNotification stores one outbox row.
// A second row with the same job, recipient, and type is rejected. A preview does not block an evaluated row.
func (p *Pool) InsertNotification(ctx context.Context, notification *domain.Notification) error {
	if notification == nil {
		return fmt.Errorf("insert notification: notification is required")
	}
	ensureID(&notification.ID)
	_, err := p.pool.Exec(ctx, `
		INSERT INTO notifications (
			id, job_id, evaluation_id, recipient, notification_type, channel, status,
			external_message_id, sent_at, error
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7,
			$8, $9, $10
		)`,
		notification.ID,
		notification.JobID,
		notification.EvaluationID,
		notification.Recipient,
		string(notification.Type),
		string(notification.Channel),
		string(notification.Status),
		notification.ExternalMessageID,
		notification.SentAt,
		notification.Error,
	)
	return mapWriteErr("insert notification", err)
}

// GetNotification reads one notification by id.
func (p *Pool) GetNotification(ctx context.Context, id string) (domain.Notification, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, job_id, evaluation_id, recipient, notification_type, channel, status,
		       external_message_id, sent_at, error
		FROM notifications
		WHERE id = $1::uuid`, id)
	notification, err := scanNotification(row)
	if err != nil {
		return domain.Notification{}, mapReadErr("get notification", err)
	}
	return notification, nil
}

func scanNotification(row rowScanner) (domain.Notification, error) {
	var notification domain.Notification
	var kind, channel, status string
	err := row.Scan(
		&notification.ID,
		&notification.JobID,
		&notification.EvaluationID,
		&notification.Recipient,
		&kind,
		&channel,
		&status,
		&notification.ExternalMessageID,
		&notification.SentAt,
		&notification.Error,
	)
	if err != nil {
		return domain.Notification{}, err
	}
	notification.Type = domain.DecisionKind(kind)
	notification.Channel = domain.NotificationChannel(channel)
	notification.Status = domain.DeliveryStatus(status)
	notification.SentAt = utcPtr(notification.SentAt)
	return notification, nil
}
