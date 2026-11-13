package notify

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/pipeline"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func (b *Bot) pollLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		next, err := b.pollOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.Logger.Error("telegram poll", "err", err)
			if !sleep(ctx, 2*time.Second) {
				return
			}
			continue
		}
		b.offset = next
	}
}

func (b *Bot) pollOnce(ctx context.Context) (int64, error) {
	if err := b.prepare(); err != nil {
		return b.offset, err
	}
	updates, err := b.Client.Updates(ctx, b.offset)
	if err != nil {
		return b.offset, err
	}
	offset := b.offset
	for _, update := range updates {
		if err := b.handle(ctx, update); err != nil {
			return offset, err
		}
		if update.UpdateID >= offset {
			offset = update.UpdateID + 1
		}
	}
	return offset, nil
}

func (b *Bot) handle(ctx context.Context, update Update) error {
	if update.Callback != nil {
		return b.handleCallback(ctx, *update.Callback)
	}
	if update.Message != nil {
		return b.handleMessage(ctx, *update.Message)
	}
	return nil
}

func (b *Bot) handleMessage(ctx context.Context, message Incoming) error {
	if !b.allowed(message.UserID, message.ChatID) {
		return nil
	}
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "/") {
		return b.command(ctx, message)
	}
	if message.ReplyID == 0 {
		return b.reply(ctx, "Reply to a preview with the posting text.")
	}
	return b.upgrade(ctx, message.ReplyID, text)
}

func (b *Bot) allowed(userID, chatID int64) bool {
	id := strconv.FormatInt(userID, 10)
	chat := strconv.FormatInt(chatID, 10)
	return id == b.UserID && chat == b.UserID
}

func (b *Bot) command(ctx context.Context, message Incoming) error {
	fields := strings.Fields(message.Text)
	name := strings.ToLower(fields[0])
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	switch name {
	case "/start", "/help":
		return b.reply(ctx, helpText)
	case "/profile":
		return b.profile(ctx)
	case "/status":
		return b.status(ctx)
	case "/sources":
		return b.sources(ctx)
	case "/feedback":
		return b.feedbackCommand(ctx, message, fields[1:])
	case "/add":
		return b.reply(ctx, "Reply to a preview with the posting text. Adding a URL is not available yet.")
	default:
		return b.reply(ctx, "Unknown command. /help lists what I can do.")
	}
}

func (b *Bot) feedbackCommand(ctx context.Context, message Incoming, args []string) error {
	kind, ok := feedbackAlias(strings.Join(args, " "))
	if message.ReplyID == 0 || !ok {
		return b.reply(ctx, "Tap a button on the message, or reply to it with /feedback interesting, not relevant, saved for later, or already applied.")
	}
	notification, err := b.Store.FindNotificationByMessage(ctx, strconv.FormatInt(message.ReplyID, 10))
	if errors.Is(err, store.ErrNotFound) {
		return b.reply(ctx, "I could not find that message.")
	}
	if err != nil {
		return err
	}
	if notification.Recipient != b.UserID {
		return nil
	}
	profile, err := b.Store.GetActiveProfile(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return b.reply(ctx, "There is no active profile.")
	}
	if err != nil {
		return err
	}
	if err := b.Store.InsertFeedback(ctx, notification.JobID, profile.ID, notification.ID, kind); err != nil {
		return err
	}
	return b.reply(ctx, "Saved")
}

func feedbackAlias(value string) (domain.FeedbackType, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "interesting":
		return domain.FeedbackInteresting, true
	case "not relevant", "not_relevant":
		return domain.FeedbackNotRelevant, true
	case "saved", "save for later", "saved_for_later":
		return domain.FeedbackSaved, true
	case "applied", "already applied", "already_applied":
		return domain.FeedbackApplied, true
	default:
		return "", false
	}
}

func (b *Bot) profile(ctx context.Context) error {
	profile, err := b.Store.GetActiveProfile(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return b.reply(ctx, "No active profile.")
	}
	if err != nil {
		return err
	}
	return b.reply(ctx, profileText(profile))
}

func (b *Bot) status(ctx context.Context) error {
	openJobs, pending, err := b.Store.NotifyStatus(ctx)
	if err != nil {
		return err
	}
	profile, err := b.Store.GetActiveProfile(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return b.reply(ctx, statusText(openJobs, pending, nil))
	}
	if err != nil {
		return err
	}
	return b.reply(ctx, statusText(openJobs, pending, &profile))
}

func (b *Bot) sources(ctx context.Context) error {
	sources, err := b.Store.ListSources(ctx)
	if err != nil {
		return err
	}
	return b.reply(ctx, sourcesText(sources))
}

func (b *Bot) upgrade(ctx context.Context, replyID int64, text string) error {
	notification, err := b.Store.FindNotificationByMessage(ctx, strconv.FormatInt(replyID, 10))
	if errors.Is(err, store.ErrNotFound) {
		return b.reply(ctx, "I could not find that preview.")
	}
	if err != nil {
		return err
	}
	if notification.Recipient != b.UserID {
		return nil
	}
	if notification.Type != domain.DecisionPreview {
		return b.reply(ctx, "That posting was already scored.")
	}
	description := strings.Join(strings.Fields(text), " ")
	if description == "" {
		return b.reply(ctx, "Send the posting text in the reply.")
	}
	job, err := b.Store.GetJob(ctx, notification.JobID)
	if err != nil {
		return err
	}
	job.Description = description
	job.Completeness = domain.CompletenessFull
	hash, err := pipeline.ContentHash(job)
	if err != nil {
		return err
	}
	if err := b.Store.UpgradeJobText(ctx, job.ID, description, hash); err != nil {
		return err
	}
	profile, err := b.Store.GetActiveProfile(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return b.reply(ctx, "Saved the text. There is no active profile to score it.")
	}
	if err != nil {
		return err
	}
	if err := b.Store.EnqueueMatch(ctx, job.ID, hash, profile.Version); err != nil {
		return err
	}
	return b.reply(ctx, "Saved the text. I'll score it.")
}

func (b *Bot) handleCallback(ctx context.Context, callback Callback) error {
	if !b.allowed(callback.UserID, callback.ChatID) {
		return nil
	}
	kind, notificationID, ok := parseFeedback(callback.Data)
	if !ok {
		return b.answer(ctx, callback.ID, "That button is not a feedback choice.")
	}
	notification, err := b.Store.GetNotification(ctx, notificationID)
	if errors.Is(err, store.ErrNotFound) {
		return b.answer(ctx, callback.ID, "That message is not available.")
	}
	if err != nil {
		return err
	}
	if notification.Recipient != b.UserID {
		return nil
	}
	profile, err := b.Store.GetActiveProfile(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return b.answer(ctx, callback.ID, "There is no active profile.")
	}
	if err != nil {
		return err
	}
	if err := b.Store.InsertFeedback(ctx, notification.JobID, profile.ID, notification.ID, kind); err != nil {
		return err
	}
	return b.answer(ctx, callback.ID, "Saved")
}

func parseFeedback(data string) (domain.FeedbackType, string, bool) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 || parts[0] != "fb" || strings.TrimSpace(parts[2]) == "" {
		return "", "", false
	}
	kind := domain.FeedbackType(parts[1])
	switch kind {
	case domain.FeedbackInteresting, domain.FeedbackNotRelevant, domain.FeedbackSaved, domain.FeedbackApplied:
		return kind, parts[2], true
	default:
		return "", "", false
	}
}

func (b *Bot) reply(ctx context.Context, text string) error {
	_, err := b.Client.Send(ctx, b.UserID, text, nil)
	if err != nil {
		b.Logger.Warn("telegram reply failed", "err", err)
	}
	return nil
}

func (b *Bot) answer(ctx context.Context, callbackID, text string) error {
	if err := b.Client.Answer(ctx, callbackID, text); err != nil {
		b.Logger.Warn("telegram answer failed", "err", err)
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
