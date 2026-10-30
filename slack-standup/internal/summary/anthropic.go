package summary

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"standup/internal/httpx"
)

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicDefaultModel   = "claude-haiku-5-5"
	anthropicVersion        = "2023-06-01"
)

// Anthropic summarises using the Claude Messages API.
type Anthropic struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
	Model   string
	Style   string
}

func NewAnthropic(client *http.Client, apiKey, model, style string) *Anthropic {
	if model == "" {
		model = anthropicDefaultModel
	}
	return &Anthropic{HTTP: client, BaseURL: anthropicDefaultBaseURL, APIKey: apiKey, Model: model, Style: style}
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (a *Anthropic) Summarise(ctx context.Context, data Data) (string, error) {
	request := anthropicRequest{
		Model:     a.Model,
		MaxTokens: maxSummaryTokens,
		Messages:  []anthropicMessage{{Role: "user", Content: buildPrompt(data, a.Style)}},
	}
	header := http.Header{
		"X-Api-Key":         {a.APIKey},
		"Anthropic-Version": {anthropicVersion},
	}

	var response anthropicResponse
	if err := httpx.PostJSON(ctx, a.HTTP, a.BaseURL+"/v1/messages", header, request, &response); err != nil {
		return "", fmt.Errorf("Anthropic summary: %w", err)
	}
	if len(response.Content) == 0 {
		return "", errors.New("Anthropic summary: response had no content")
	}
	return strings.TrimSpace(response.Content[0].Text), nil
}
