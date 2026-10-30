package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes a config whose notes file holds one completed item dated today.
func writeConfig(t *testing.T) (configPath string) {
	t.Helper()

	dir := t.TempDir()
	notesPath := filepath.Join(dir, "notes.md")
	notes := fmt.Sprintf("## %s\n### Completed\n- Wrote the migration guide\n### Plan\n- Review the release PR\n",
		time.Now().Format("2006-01-02"))
	if err := os.WriteFile(notesPath, []byte(notes), 0o644); err != nil {
		t.Fatal(err)
	}

	configPath = filepath.Join(dir, "standup_config.toml")
	contents := fmt.Sprintf("[notes]\nfile_path = %q\n\n[ai_summary]\nenabled = false\n", notesPath)
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	// With no tokens set, every API source is skipped and no network is used.
	for _, name := range []string{"GITHUB_TOKEN", "JIRA_TOKEN", "CONFLUENCE_TOKEN", "SLACK_WEBHOOK_URL", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
	}
	return configPath
}

func TestDryRunPrintsAPreviewBuiltFromNotes(t *testing.T) {
	configPath := writeConfig(t)
	var stdout, stderr bytes.Buffer

	err := run(context.Background(), []string{"--dry-run", "--config", configPath}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr.String())
	}
	for _, want := range []string{
		"DRY RUN - PREVIEW OF STANDUP MESSAGE:",
		"Wrote the migration guide",
		"Review the release PR",
		"Dry run complete",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q\n%s", want, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), "Source unavailable") {
		t.Errorf("stderr should warn about the sources without tokens:\n%s", stderr.String())
	}
}

func TestDryRunWinsOverPost(t *testing.T) {
	configPath := writeConfig(t)
	var stdout, stderr bytes.Buffer

	err := run(context.Background(), []string{"--dry-run", "--post", "--config", configPath}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("run() error = %v, want no error because nothing is posted in a dry run", err)
	}
	if !strings.Contains(stderr.String(), "Skipping Slack post") {
		t.Errorf("stderr should say the post was skipped:\n%s", stderr.String())
	}
}

func TestPostWithoutAWebhookFailsAfterPrintingTheMessage(t *testing.T) {
	configPath := writeConfig(t)
	var stdout, stderr bytes.Buffer

	err := run(context.Background(), []string{"--post", "--config", configPath}, &stdout, &stderr)

	if err == nil || !strings.Contains(err.Error(), "webhook") {
		t.Errorf("run() error = %v, want a missing-webhook error", err)
	}
	if !strings.Contains(stdout.String(), "Wrote the migration guide") {
		t.Errorf("the message should still be printed:\n%s", stdout.String())
	}
}

func TestFirstRunCreatesTheDefaultConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "standup_config.toml")
	for _, name := range []string{"GITHUB_TOKEN", "JIRA_TOKEN", "CONFLUENCE_TOKEN", "SLACK_WEBHOOK_URL", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
	}
	var stdout, stderr bytes.Buffer

	err := run(context.Background(), []string{"--dry-run", "--config", configPath}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if _, statErr := os.Stat(configPath); statErr != nil {
		t.Errorf("default config was not written: %v", statErr)
	}
	if !strings.Contains(stderr.String(), "Created default config") {
		t.Errorf("stderr should ask the user to update the new config:\n%s", stderr.String())
	}
}
