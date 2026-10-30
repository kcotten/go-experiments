// Command standup generates a daily standup from GitHub, Jira, Confluence and
// personal notes, and optionally posts it to Slack.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"standup/internal/config"
	"standup/internal/httpx"
	"standup/internal/render"
	"standup/internal/slack"
	"standup/internal/source/confluence"
	"standup/internal/source/github"
	"standup/internal/source/jira"
	"standup/internal/standup"
	"standup/internal/summary"
)

const reportRuleWidth = 50

type options struct {
	post       bool
	dryRun     bool
	debug      bool
	configPath string
	channel    string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("standup", flag.ContinueOnError)
	flags.SetOutput(stderr)

	flags.BoolVar(&opts.post, "post", false, "Post to Slack automatically")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "Preview mode - fetch data but do not post to Slack")
	flags.BoolVar(&opts.debug, "debug", false, "Show detailed debug information")
	flags.StringVar(&opts.configPath, "config", config.DefaultPath, "Configuration file path")
	flags.StringVar(&opts.channel, "channel", "", "Slack channel or user to post to (overrides the config file)")

	err := flags.Parse(args)
	return opts, err
}

func newLogger(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	logger := newLogger(stderr, opts.debug)

	cfg, created, err := config.LoadOrCreate(opts.configPath)
	if err != nil {
		return err
	}
	if created {
		logger.Warn("Created default config. Please update with your details.", "path", opts.configPath)
	}
	if opts.channel != "" {
		cfg.Slack.Channel = opts.channel
	}
	secrets := config.SecretsFromEnv()

	// Requests to these APIs are logged at debug level. The Slack webhook URL
	// is a secret, so Slack gets a client that logs nothing.
	apiClient := httpx.NewLoggingClient(logger)

	// Dry run takes precedence over post.
	shouldPost := opts.post && !opts.dryRun
	if opts.dryRun {
		logger.Info("DRY RUN MODE - no data will be posted to Slack")
	}

	gatherer := standup.Gatherer{
		GitHub:     github.New(apiClient, secrets.GitHubToken, cfg.GitHub.Username, cfg.GitHub.Repos),
		Jira:       jira.New(apiClient, cfg.Jira.BaseURL, cfg.Jira.Username, secrets.JiraToken),
		Confluence: confluence.New(apiClient, cfg.Confluence.BaseURL, cfg.Confluence.Username, secrets.ConfluenceToken),
		NotesPath:  cfg.Notes.FilePath,
		Logger:     logger,
	}

	now := time.Now()
	gathered := gatherer.Gather(ctx, now)
	aiSummary := generateSummary(ctx, logger, cfg.AISummary, secrets, apiClient, gathered)

	logger.Info("Formatting standup message...")
	input := gathered.Input(now, aiSummary)
	blocks := render.Blocks(input)
	message := render.Text(input)
	logDebugBlocks(ctx, logger, blocks)

	var postErr error
	switch {
	case shouldPost:
		logger.Info("Posting to Slack...")
		poster := slack.New(httpx.NewClient(), secrets.SlackWebhookURL, cfg.Slack)
		postErr = standup.Post(ctx, poster, blocks, message, logger)
	case opts.post && opts.dryRun:
		logger.Info("Skipping Slack post due to dry run mode")
	}

	printReport(stdout, opts, message)

	if postErr != nil {
		return fmt.Errorf("posting to Slack: %w", postErr)
	}
	return nil
}

// generateSummary returns the AI summary text, or "" when summaries are
// disabled so the detailed breakdown is shown instead.
func generateSummary(
	ctx context.Context,
	logger *slog.Logger,
	cfg config.AISummary,
	secrets config.Secrets,
	client *http.Client,
	gathered standup.Activity,
) string {
	if !cfg.Enabled {
		logger.Debug("AI summary disabled in config")
		return ""
	}

	logger.Info("Generating AI summary...")
	summariser, err := summary.For(cfg, secrets, client)
	if err != nil {
		logger.Warn("AI provider unavailable; using a simple summary", "error", err)
	}

	text, err := summary.Generate(ctx, summariser, gathered.SummaryData())
	if err != nil {
		logger.Error("AI summary failed", "error", err)
	}
	logger.Debug("AI summary generated", "summary", text)
	return text
}

// logDebugBlocks logs the Slack payload as JSON, which is how Slack receives it.
func logDebugBlocks(ctx context.Context, logger *slog.Logger, blocks []render.Block) {
	if !logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		logger.Debug("Could not encode Slack blocks", "error", err)
		return
	}
	logger.Debug("Slack blocks", "blocks", string(encoded))
}

func printReport(w io.Writer, opts options, message string) {
	rule := strings.Repeat("=", reportRuleWidth)
	heading := "GENERATED STANDUP MESSAGE:"
	if opts.dryRun {
		heading = "DRY RUN - PREVIEW OF STANDUP MESSAGE:"
	}
	fmt.Fprintf(w, "\n%s\n%s\n%s\n%s\n", rule, heading, rule, message)

	switch {
	case opts.dryRun:
		fmt.Fprintln(w, "\n✅ Dry run complete! Use --post to actually send to Slack")
		if !opts.debug {
			fmt.Fprintln(w, "💡 Use --debug to see detailed API information")
		}
	case !opts.post:
		fmt.Fprintln(w, "\n💡 Options:")
		fmt.Fprintln(w, "  --dry-run    Preview with real data (safe for testing)")
		fmt.Fprintln(w, "  --debug      Show debug information")
		fmt.Fprintln(w, "  --post       Generate and post to Slack")
	}
}
