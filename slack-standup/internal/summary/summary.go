// Package summary condenses a day's activity into a short written summary,
// using an AI provider when one is configured and simple counts otherwise.
package summary

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"standup/internal/activity"
	"standup/internal/config"
)

const (
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
	providerNone      = "none"

	noActivityToSummarise = "No significant activity found to summarize."
	noActivityToReport    = "No significant activity to report."
)

// Data is the activity handed to a summariser. Unavailable sources are empty.
type Data struct {
	GitHub     activity.GitHub
	Jira       activity.Jira
	Confluence activity.Confluence
	// Notes are the completed items from the personal notes file.
	Notes []string
}

func (d Data) itemCount() int {
	return len(d.GitHub.Commits) +
		len(d.GitHub.PRsOpened) +
		len(d.GitHub.PRsMerged) +
		len(d.GitHub.Reviews) +
		len(d.Jira.Completed) +
		len(d.Jira.Updated) +
		len(d.Jira.InProgress) +
		len(d.Confluence.Created) +
		len(d.Confluence.Updated) +
		len(d.Notes)
}

// Summariser turns activity into a short written summary.
type Summariser interface {
	Summarise(ctx context.Context, data Data) (string, error)
}

// Generate summarises data. The returned text is always usable: when the
// summariser fails it is the simple count-based summary, and err says why.
func Generate(ctx context.Context, summariser Summariser, data Data) (text string, err error) {
	if data.itemCount() == 0 {
		return noActivityToSummarise, nil
	}

	text, err = summariser.Summarise(ctx, data)
	if err != nil {
		return simpleSummary(data), fmt.Errorf("using simple summary instead: %w", err)
	}
	return text, nil
}

// For picks the summariser named by the config. When the provider cannot be
// used (unknown name, missing API key) it returns Simple along with an error
// explaining why.
func For(cfg config.AISummary, secrets config.Secrets, client *http.Client) (Summariser, error) {
	switch cfg.Provider {
	case providerOpenAI:
		if secrets.OpenAIAPIKey == "" {
			return Simple{}, fmt.Errorf("provider %q needs OPENAI_API_KEY", cfg.Provider)
		}
		return NewOpenAI(client, secrets.OpenAIAPIKey, cfg.Model, cfg.PromptStyle), nil
	case providerAnthropic:
		if secrets.AnthropicAPIKey == "" {
			return Simple{}, fmt.Errorf("provider %q needs ANTHROPIC_API_KEY", cfg.Provider)
		}
		return NewAnthropic(client, secrets.AnthropicAPIKey, cfg.Model, cfg.PromptStyle), nil
	case providerNone, "":
		return Simple{}, nil
	default:
		return Simple{}, fmt.Errorf("unknown ai_summary provider %q", cfg.Provider)
	}
}

// Simple summarises by counting items, with no external calls.
type Simple struct{}

func (Simple) Summarise(_ context.Context, data Data) (string, error) {
	return simpleSummary(data), nil
}

func simpleSummary(data Data) string {
	counts := []struct {
		count int
		label string
	}{
		{len(data.GitHub.Commits), "commits"},
		{len(data.GitHub.PRsMerged) + len(data.GitHub.PRsOpened), "PRs"},
		{len(data.GitHub.Reviews), "reviews"},
		{len(data.Jira.Completed) + len(data.Jira.Updated), "Jira tickets"},
		{len(data.Notes), "other tasks"},
	}

	var parts []string
	for _, entry := range counts {
		if entry.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", entry.count, entry.label))
		}
	}
	if len(parts) == 0 {
		return noActivityToReport
	}
	return fmt.Sprintf("Completed: %s.", strings.Join(parts, ", "))
}
