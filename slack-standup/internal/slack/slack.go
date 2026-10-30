// Package slack posts standup messages through an incoming webhook.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"standup/internal/config"
	"standup/internal/render"
)

const maxErrorBodyBytes = 200

// ErrNoWebhook is returned when no webhook URL has been configured.
var ErrNoWebhook = errors.New("Slack webhook URL not configured")

type Client struct {
	HTTP       *http.Client
	WebhookURL string
	Channel    string
	Username   string
	IconEmoji  string
}

func New(client *http.Client, webhookURL string, cfg config.Slack) *Client {
	return &Client{
		HTTP:       client,
		WebhookURL: webhookURL,
		Channel:    cfg.Channel,
		Username:   cfg.Username,
		IconEmoji:  cfg.IconEmoji,
	}
}

type payload struct {
	Channel   string         `json:"channel"`
	Username  string         `json:"username"`
	IconEmoji string         `json:"icon_emoji"`
	Blocks    []render.Block `json:"blocks,omitempty"`
	Text      string         `json:"text,omitempty"`
}

// PostBlocks posts a Block Kit message.
func (c *Client) PostBlocks(ctx context.Context, blocks []render.Block) error {
	if len(blocks) == 0 {
		return errors.New("no blocks to post to Slack")
	}
	return c.post(ctx, payload{Blocks: blocks})
}

// PostText posts a plain-text message.
func (c *Client) PostText(ctx context.Context, text string) error {
	if text == "" {
		return errors.New("no text to post to Slack")
	}
	return c.post(ctx, payload{Text: text})
}

// post sends the payload. The webhook URL is a secret, so it is kept out of
// every error message.
func (c *Client) post(ctx context.Context, message payload) error {
	if c.WebhookURL == "" {
		return ErrNoWebhook
	}

	message.Channel = c.Channel
	message.Username = c.Username
	message.IconEmoji = c.IconEmoji

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encoding Slack message: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("building Slack request: webhook URL is invalid")
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.HTTP.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // drop the URL the client attaches
		}
		return fmt.Errorf("posting to Slack: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		return fmt.Errorf("posting to Slack: HTTP %d %s", response.StatusCode, bytes.TrimSpace(errorBody))
	}
	return nil
}
