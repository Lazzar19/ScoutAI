package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendUsesPlainTextAndButtons(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body error = %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77}}`))
	}))
	defer server.Close()

	id, err := (Client{Token: "test-token", BaseURL: server.URL, HTTP: server.Client()}).Send(
		context.Background(),
		"42",
		"Go_Intern *_*[v1]*",
		FeedbackButtons("note-1"),
	)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "77" {
		t.Fatalf("message id = %s", id)
	}
	if _, ok := got["parse_mode"]; ok {
		t.Fatalf("parse_mode was set: %+v", got["parse_mode"])
	}
	if got["text"] != "Go_Intern *_*[v1]*" || got["chat_id"] != "42" {
		t.Fatalf("payload = %+v", got)
	}
	markup, _ := got["reply_markup"].(map[string]any)
	keyboard, _ := markup["inline_keyboard"].([]any)
	if len(keyboard) != 2 {
		t.Fatalf("keyboard = %+v", markup)
	}
}
