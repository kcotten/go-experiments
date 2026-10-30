// Package config loads the TOML configuration file and environment secrets.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

const DefaultPath = "standup_config.toml"

type Config struct {
	GitHub     GitHub     `toml:"github"`
	Jira       Jira       `toml:"jira"`
	Confluence Confluence `toml:"confluence"`
	Notes      Notes      `toml:"notes"`
	Slack      Slack      `toml:"slack"`
	AISummary  AISummary  `toml:"ai_summary"`
}

type GitHub struct {
	Username       string   `toml:"username"`
	Repos          []string `toml:"repos"`
	IncludePRs     bool     `toml:"include_prs"`
	IncludeCommits bool     `toml:"include_commits"`
	IncludeReviews bool     `toml:"include_reviews"`
}

type Jira struct {
	BaseURL           string   `toml:"base_url"`
	Username          string   `toml:"username"`
	ProjectKeys       []string `toml:"project_keys"`
	IncludeCompleted  bool     `toml:"include_completed"`
	IncludeInProgress bool     `toml:"include_in_progress"`
}

type Confluence struct {
	BaseURL  string `toml:"base_url"`
	Username string `toml:"username"`
}

type Notes struct {
	FilePath string `toml:"file_path"`
	Format   string `toml:"format"`
}

type Slack struct {
	Channel   string `toml:"channel"`
	Username  string `toml:"username"`
	IconEmoji string `toml:"icon_emoji"`
}

type AISummary struct {
	Enabled bool `toml:"enabled"`
	// Provider is "openai", "anthropic" or "none".
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	// PromptStyle is "professional", "casual" or "detailed".
	PromptStyle string `toml:"prompt_style"`
}

// Default returns the configuration written on first run.
func Default() Config {
	return Config{
		GitHub: GitHub{
			Username:       "your-username",
			Repos:          []string{"org/repo1", "org/repo2"},
			IncludePRs:     true,
			IncludeCommits: true,
			IncludeReviews: true,
		},
		Jira: Jira{
			BaseURL:           "https://your-org.atlassian.net",
			Username:          "your-email@company.com",
			ProjectKeys:       []string{"PROJ", "TEAM"},
			IncludeCompleted:  true,
			IncludeInProgress: true,
		},
		Confluence: Confluence{
			BaseURL:  "https://your-org.atlassian.net",
			Username: "your-email@company.com",
		},
		Notes: Notes{FilePath: "./daily_notes.md", Format: "markdown"},
		Slack: Slack{
			Channel:   "#standup",
			Username:  "Standup Bot",
			IconEmoji: ":robot_face:",
		},
		AISummary: AISummary{
			Enabled:     true,
			Provider:    "none",
			Model:       "claude-haiku-5-5",
			PromptStyle: "professional",
		},
	}
}

// LoadOrCreate reads the config at path. When the file does not exist it
// writes the default config there and returns it with created set to true,
// so the caller can ask the user to fill in their details.
func LoadOrCreate(path string) (cfg Config, created bool, err error) {
	_, err = toml.DecodeFile(path, &cfg)
	if err == nil {
		return cfg, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, fmt.Errorf("loading config %q: %w", path, err)
	}

	cfg = Default()
	if err := write(path, cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func write(path string, cfg Config) error {
	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(cfg); err != nil {
		return fmt.Errorf("encoding default config: %w", err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing default config %q: %w", path, err)
	}
	return nil
}

// Secrets are the API tokens read from the environment. Empty means unset.
type Secrets struct {
	GitHubToken     string
	JiraToken       string
	ConfluenceToken string
	SlackWebhookURL string
	OpenAIAPIKey    string
	AnthropicAPIKey string
}

func SecretsFromEnv() Secrets {
	return Secrets{
		GitHubToken:     os.Getenv("GITHUB_TOKEN"),
		JiraToken:       os.Getenv("JIRA_TOKEN"),
		ConfluenceToken: os.Getenv("CONFLUENCE_TOKEN"),
		SlackWebhookURL: os.Getenv("SLACK_WEBHOOK_URL"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
	}
}
