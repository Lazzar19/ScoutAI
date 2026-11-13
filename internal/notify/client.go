package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Button is one inline keyboard button. Data is sent back with the tap.
type Button struct {
	Text string
	Data string
}

// API is the Telegram bot methods this process uses.
type API interface {
	Send(ctx context.Context, chatID, text string, buttons [][]Button) (string, error)
	Answer(ctx context.Context, callbackID, text string) error
	Updates(ctx context.Context, offset int64) ([]Update, error)
}

// Update is one long-poll result.
type Update struct {
	UpdateID int64
	Message  *Incoming
	Callback *Callback
}

// Incoming is a private message from a user.
type Incoming struct {
	MessageID int64
	UserID    int64
	ChatID    int64
	Text      string
	ReplyID   int64
}

// Callback is a tap on an inline button.
type Callback struct {
	ID        string
	UserID    int64
	ChatID    int64
	Data      string
	MessageID int64
}

// Client calls the bot HTTP API. The token stays in the request path and is not logged.
type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

// Send posts a plain-text message. Parse mode is left unset so titles keep their punctuation.
func (c Client) Send(ctx context.Context, chatID, text string, buttons [][]Button) (string, error) {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if len(buttons) > 0 {
		rows := make([][]map[string]string, len(buttons))
		for i, row := range buttons {
			rows[i] = make([]map[string]string, len(row))
			for j, button := range row {
				rows[i][j] = map[string]string{"text": button.Text, "callback_data": button.Data}
			}
		}
		payload["reply_markup"] = map[string]any{"inline_keyboard": rows}
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	if err := c.call(ctx, "sendMessage", payload, 15*time.Second, &sent); err != nil {
		return "", err
	}
	if sent.MessageID == 0 {
		return "", fmt.Errorf("telegram: send returned no message id")
	}
	return fmt.Sprintf("%d", sent.MessageID), nil
}

// Answer closes the button spinner.
func (c Client) Answer(ctx context.Context, callbackID, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]string{
		"callback_query_id": callbackID,
		"text":              text,
	}, 15*time.Second, nil)
}

// Updates waits for the next private messages and button taps.
func (c Client) Updates(ctx context.Context, offset int64) ([]Update, error) {
	payload := map[string]any{
		"timeout":         25,
		"allowed_updates": []string{"message", "callback_query"},
	}
	if offset > 0 {
		payload["offset"] = offset
	}
	var raw []tgUpdate
	if err := c.call(ctx, "getUpdates", payload, 40*time.Second, &raw); err != nil {
		return nil, err
	}
	updates := make([]Update, 0, len(raw))
	for _, item := range raw {
		updates = append(updates, item.export())
	}
	return updates, nil
}

func (c Client) call(ctx context.Context, method string, payload any, timeout time.Duration, dest any) error {
	if strings.TrimSpace(c.Token) == "" || strings.Contains(c.Token, "/") {
		return fmt.Errorf("telegram: token is invalid")
	}
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: encode %s: %w", method, err)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, base+"/bot"+c.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ScoutAI")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return &APIError{Temporary: true, Description: err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &APIError{Status: resp.StatusCode, Temporary: true, Description: err.Error()}
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return &APIError{Status: resp.StatusCode, Temporary: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500, Description: "response was not JSON"}
	}
	if resp.StatusCode != http.StatusOK || !envelope.OK {
		return &APIError{
			Status:      resp.StatusCode,
			Code:        envelope.ErrorCode,
			Description: envelope.Description,
			Temporary:   resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		}
	}
	if dest != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, dest); err != nil {
			return fmt.Errorf("telegram: decode %s: %w", method, err)
		}
	}
	return nil
}

// APIError is a Telegram response. Temporary errors stay pending and are tried again.
type APIError struct {
	Status      int
	Code        int
	Description string
	Temporary   bool
}

func (e *APIError) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("telegram: status %d", e.Status)
	}
	return "telegram: " + e.Description
}

type tgUpdate struct {
	UpdateID int64       `json:"update_id"`
	Message  *tgMessage  `json:"message"`
	Callback *tgCallback `json:"callback_query"`
}

type tgMessage struct {
	MessageID int64      `json:"message_id"`
	From      *tgUser    `json:"from"`
	Chat      tgChat     `json:"chat"`
	Text      string     `json:"text"`
	Reply     *tgMessage `json:"reply_to_message"`
}

type tgCallback struct {
	ID      string     `json:"id"`
	From    *tgUser    `json:"from"`
	Data    string     `json:"data"`
	Message *tgMessage `json:"message"`
}

type tgUser struct {
	ID int64 `json:"id"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

func (u tgUpdate) export() Update {
	out := Update{UpdateID: u.UpdateID}
	if u.Message != nil {
		out.Message = u.Message.export()
	}
	if u.Callback != nil {
		callback := Callback{ID: u.Callback.ID, Data: u.Callback.Data}
		if u.Callback.From != nil {
			callback.UserID = u.Callback.From.ID
		}
		if u.Callback.Message != nil {
			callback.ChatID = u.Callback.Message.Chat.ID
			callback.MessageID = u.Callback.Message.MessageID
		}
		out.Callback = &callback
	}
	return out
}

func (m tgMessage) export() *Incoming {
	incoming := &Incoming{MessageID: m.MessageID, ChatID: m.Chat.ID, Text: m.Text}
	if m.From != nil {
		incoming.UserID = m.From.ID
	}
	if m.Reply != nil {
		incoming.ReplyID = m.Reply.MessageID
	}
	return incoming
}
