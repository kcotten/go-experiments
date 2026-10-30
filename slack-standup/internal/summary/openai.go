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
	openAIDefaultBaseURL = "https://api.openai.com"
	openAIDefaultModel   = "gpt-4o-mini"
	openAISystemPrompt   = "You are a helpful assistant that creates concise, professional daily standup summaries."

	maxSummaryTokens  = 300
	openAITemperature = 0.3
)

// OpenAI summarises using the chat completions API.
type OpenAI struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
	Model   string
	Style   string
}

func NewOpenAI(client *http.Client, apiKey, model, style string) *OpenAI {
	if model == "" {
		model = openAIDefaultModel
	}
	return &OpenAI{HTTP: client, BaseURL: openAIDefaultBaseURL, APIKey: apiKey, Model: model, Style: style}
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

func (o *OpenAI) Summarise(ctx context.Context, data Data) (string, error) {
	request := openAIRequest{
		Model: o.Model,
		Messages: []openAIMessage{
			{Role: "system", Content: openAISystemPrompt},
			{Role: "user", Content: buildPrompt(data, o.Style)},
		},
		MaxTokens:   maxSummaryTokens,
		Temperature: openAITemperature,
	}
	header := http.Header{"Authorization": {"Bearer " + o.APIKey}}

	var response openAIResponse
	if err := httpx.PostJSON(ctx, o.HTTP, o.BaseURL+"/v1/chat/completions", header, request, &response); err != nil {
		return "", fmt.Errorf("OpenAI summary: %w", err)
	}
	if len(response.Choices) == 0 {
		return "", errors.New("OpenAI summary: response had no choices")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}
