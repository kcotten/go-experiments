package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"standup/internal/config"
	"standup/internal/render"
)

var slackConfig = config.Slack{Channel: "#standup", Username: "Standup Bot", IconEmoji: ":robot_face:"}

func TestPostBlocksSendsTheConfiguredIdentityAndBlocks(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte("ok")) // Slack replies with plain text, not JSON
	}))
	defer server.Close()
	client := New(server.Client(), server.URL, slackConfig)

	err := client.PostBlocks(context.Background(), []render.Block{{Type: "divider"}})

	if err != nil {
		t.Fatalf("PostBlocks() error = %v", err)
	}
	if got["channel"] != "#standup" || got["username"] != "Standup Bot" || got["icon_emoji"] != ":robot_face:" {
		t.Errorf("payload = %v, want the configured channel and identity", got)
	}
	if blocks, ok := got["blocks"].([]any); !ok || len(blocks) != 1 {
		t.Errorf("payload blocks = %v, want one block", got["blocks"])
	}
	if _, hasText := got["text"]; hasText {
		t.Error("payload has text alongside blocks, want blocks only")
	}
}

func TestPostTextSendsTheMessage(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer server.Close()
	client := New(server.Client(), server.URL, slackConfig)

	if err := client.PostText(context.Background(), "hello"); err != nil {
		t.Fatalf("PostText() error = %v", err)
	}
	if got["text"] != "hello" {
		t.Errorf("payload = %v, want text %q", got, "hello")
	}
}

func TestPostReportsSlackRejectionsWithoutLeakingTheWebhookURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_blocks", http.StatusBadRequest)
	}))
	defer server.Close()
	secretURL := server.URL + "/services/T000/B000/super-secret"
	client := New(server.Client(), secretURL, slackConfig)

	err := client.PostText(context.Background(), "hello")

	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "invalid_blocks") {
		t.Fatalf("PostText() error = %v, want the status and Slack's reason", err)
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("error %q leaks the webhook URL", err)
	}
}

func TestPostDoesNotLeakTheWebhookURLOnConnectionFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	secretURL := server.URL + "/services/super-secret"
	server.Close() // nothing is listening any more
	client := New(http.DefaultClient, secretURL, slackConfig)

	err := client.PostText(context.Background(), "hello")

	if err == nil {
		t.Fatal("PostText() error = nil, want a connection error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("error %q leaks the webhook URL", err)
	}
}

func TestPostRequiresAWebhook(t *testing.T) {
	client := New(http.DefaultClient, "", slackConfig)

	if err := client.PostText(context.Background(), "hello"); !errors.Is(err, ErrNoWebhook) {
		t.Errorf("PostText() error = %v, want ErrNoWebhook", err)
	}
}
