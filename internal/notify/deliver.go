package notify

import (
	"context"
	"errors"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func (b *Bot) deliverLoop(ctx context.Context) {
	if err := b.DeliverOnce(ctx); err != nil && ctx.Err() == nil {
		b.Logger.Error("deliver notifications", "err", err)
	}
	ticker := time.NewTicker(b.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.DeliverOnce(ctx); err != nil && ctx.Err() == nil {
				b.Logger.Error("deliver notifications", "err", err)
			}
		}
	}
}

// DeliverOnce sends due pending notifications for the allowed user.
func (b *Bot) DeliverOnce(ctx context.Context) error {
	if err := b.prepare(); err != nil {
		return err
	}
	pending, err := b.Store.ListPendingNotifications(ctx, b.UserID, b.enabledTypes(), 20)
	if err != nil {
		return err
	}
	now := b.Now()
	for _, notification := range pending {
		if !b.due(notification.ID, now) {
			continue
		}
		if err := b.deliver(ctx, notification); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) enabledTypes() []domain.DecisionKind {
	var types []domain.DecisionKind
	if b.Preview {
		types = append(types, domain.DecisionPreview)
	}
	if b.Evaluated {
		types = append(types, domain.DecisionEvaluated)
	}
	return types
}

func (b *Bot) due(id string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	next, ok := b.next[id]
	return !ok || !now.Before(next)
}

func (b *Bot) deliver(ctx context.Context, notification domain.Notification) error {
	job, err := b.Store.GetJob(ctx, notification.JobID)
	if errors.Is(err, store.ErrNotFound) {
		return b.fail(ctx, notification.ID, "job was not found")
	}
	if err != nil {
		return err
	}
	sourceName, originalURL, err := b.Store.JobOrigin(ctx, job.ID)
	if errors.Is(err, store.ErrNotFound) {
		sourceName = ""
		originalURL = job.URL
	} else if err != nil {
		return err
	}
	if originalURL == "" {
		originalURL = job.URL
	}
	text, err := b.messageText(ctx, notification, job, sourceName, originalURL)
	if errors.Is(err, errSkip) {
		return nil
	}
	if err != nil {
		return err
	}
	messageID, err := b.Client.Send(ctx, notification.Recipient, text, FeedbackButtons(notification.ID))
	if err != nil {
		return b.afterSendError(ctx, notification.ID, err)
	}
	if err := b.Store.MarkNotificationSent(ctx, notification.ID, messageID, b.Now().UTC()); err != nil {
		return err
	}
	b.forget(notification.ID)
	b.Logger.Info("notification sent", "notification_id", notification.ID, "type", notification.Type)
	return nil
}

func (b *Bot) messageText(ctx context.Context, notification domain.Notification, job domain.Job, sourceName, originalURL string) (string, error) {
	switch notification.Type {
	case domain.DecisionPreview:
		return PreviewText(job, sourceName, originalURL), nil
	case domain.DecisionEvaluated:
		score, err := b.score(ctx, notification)
		if err != nil {
			return "", err
		}
		return EvaluatedText(job, score, sourceName, originalURL), nil
	default:
		return "", b.skip(ctx, notification.ID, "unknown notification type")
	}
}

func (b *Bot) score(ctx context.Context, notification domain.Notification) (int, error) {
	if notification.EvaluationID == nil || *notification.EvaluationID == "" {
		return 0, b.skip(ctx, notification.ID, "evaluated notification has no evaluation")
	}
	evaluation, err := b.Store.GetEvaluation(ctx, *notification.EvaluationID)
	if errors.Is(err, store.ErrNotFound) {
		return 0, b.skip(ctx, notification.ID, "evaluation was not found")
	}
	if err != nil {
		return 0, err
	}
	if evaluation.OverallScore == nil {
		return 0, b.skip(ctx, notification.ID, "evaluated notification has no score")
	}
	return *evaluation.OverallScore, nil
}

var errSkip = errors.New("notification skipped")

func (b *Bot) skip(ctx context.Context, id, reason string) error {
	if err := b.fail(ctx, id, reason); err != nil {
		return err
	}
	return errSkip
}

func (b *Bot) afterSendError(ctx context.Context, id string, cause error) error {
	var apiErr *APIError
	if errors.As(cause, &apiErr) && apiErr.Temporary {
		return b.retry(ctx, id, cause)
	}
	if apiErr != nil {
		return b.fail(ctx, id, cause.Error())
	}
	return b.retry(ctx, id, cause)
}

func (b *Bot) retry(ctx context.Context, id string, cause error) error {
	b.mu.Lock()
	b.tries[id]++
	attempt := b.tries[id]
	wait := b.RetryBase
	if attempt > 1 && wait > 0 {
		shift := attempt - 1
		if shift > 5 {
			shift = 5
		}
		wait *= time.Duration(1 << shift)
	}
	b.next[id] = b.Now().Add(wait)
	b.mu.Unlock()
	if attempt >= b.MaxAttempts {
		return b.fail(ctx, id, cause.Error())
	}
	if err := b.Store.MarkNotificationRetry(ctx, id, cause.Error()); err != nil {
		return err
	}
	b.Logger.Warn("notification delivery delayed", "notification_id", id, "err", cause)
	return nil
}

func (b *Bot) fail(ctx context.Context, id, reason string) error {
	b.forget(id)
	if err := b.Store.MarkNotificationFailed(ctx, id, reason); err != nil {
		return err
	}
	b.Logger.Error("notification failed", "notification_id", id, "err", reason)
	return nil
}

func (b *Bot) forget(id string) {
	b.mu.Lock()
	delete(b.next, id)
	delete(b.tries, id)
	b.mu.Unlock()
}
