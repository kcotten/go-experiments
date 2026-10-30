package standup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
	"standup/internal/render"
)

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeGitHub struct {
	data activity.GitHub
	err  error
}

func (f fakeGitHub) Activity(context.Context, time.Time) (activity.GitHub, error) {
	return f.data, f.err
}

type fakeJira struct {
	data activity.Jira
	err  error
}

func (f fakeJira) Activity(context.Context, time.Time) (activity.Jira, error) {
	return f.data, f.err
}

type fakeConfluence struct {
	data activity.Confluence
	err  error
}

func (f fakeConfluence) Activity(context.Context, time.Time) (activity.Confluence, error) {
	return f.data, f.err
}

func TestGatherTreatsAMissingTokenAsAnUnavailableSource(t *testing.T) {
	gatherer := Gatherer{
		GitHub:     fakeGitHub{err: fmt.Errorf("GitHub: %w", httpx.ErrMissingToken)},
		Jira:       fakeJira{data: activity.Jira{Completed: []activity.Ticket{{Key: "PROJ-1"}}}},
		Confluence: fakeConfluence{err: httpx.ErrMissingToken},
		NotesPath:  filepath.Join(t.TempDir(), "missing.md"),
		Logger:     quietLogger,
	}

	got := gatherer.Gather(context.Background(), time.Now())

	if got.GitHub != nil || got.Confluence != nil {
		t.Errorf("GitHub = %v, Confluence = %v, want both nil", got.GitHub, got.Confluence)
	}
	if got.Jira == nil || len(got.Jira.Completed) != 1 {
		t.Errorf("Jira = %+v, want the available source's data", got.Jira)
	}
}

func TestGatherKeepsPartialResultsFromAFailingSource(t *testing.T) {
	partial := activity.GitHub{Commits: []activity.Commit{{Message: "Kept"}}}
	gatherer := Gatherer{
		GitHub:     fakeGitHub{data: partial, err: errors.New("one repo failed")},
		Jira:       fakeJira{},
		Confluence: fakeConfluence{},
		NotesPath:  filepath.Join(t.TempDir(), "missing.md"),
		Logger:     quietLogger,
	}

	got := gatherer.Gather(context.Background(), time.Now())

	if got.GitHub == nil || len(got.GitHub.Commits) != 1 {
		t.Errorf("GitHub = %+v, want the partial commits", got.GitHub)
	}
}

func TestGatherReadsTodaysNotes(t *testing.T) {
	now := time.Date(2025, 3, 12, 9, 0, 0, 0, time.UTC)
	notesPath := filepath.Join(t.TempDir(), "notes.md")
	content := "## 2025-03-11\n### Completed\n- Shipped importer\n## 2025-03-12\n### Plan\n- Review PRs\n"
	if err := os.WriteFile(notesPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gatherer := Gatherer{
		GitHub: fakeGitHub{}, Jira: fakeJira{}, Confluence: fakeConfluence{},
		NotesPath: notesPath, Logger: quietLogger,
	}

	got := gatherer.Gather(context.Background(), now)

	if len(got.Notes.Completed) != 1 || got.Notes.Completed[0] != "Shipped importer" {
		t.Errorf("Notes.Completed = %q, want yesterday's item", got.Notes.Completed)
	}
	if len(got.Notes.Plan) != 1 || got.Notes.Plan[0] != "Review PRs" {
		t.Errorf("Notes.Plan = %q, want today's plan", got.Notes.Plan)
	}
}

type fakePoster struct {
	blocksErr, textErr error
	postedBlocks       bool
	postedText         string
}

func (f *fakePoster) PostBlocks(context.Context, []render.Block) error {
	f.postedBlocks = f.blocksErr == nil
	return f.blocksErr
}

func (f *fakePoster) PostText(_ context.Context, text string) error {
	if f.textErr == nil {
		f.postedText = text
	}
	return f.textErr
}

func TestPostPrefersRichBlocks(t *testing.T) {
	poster := &fakePoster{}

	err := Post(context.Background(), poster, []render.Block{{Type: "divider"}}, "fallback", quietLogger)

	if err != nil || !poster.postedBlocks || poster.postedText != "" {
		t.Errorf("Post() = %v, blocks posted = %v, text = %q; want blocks only", err, poster.postedBlocks, poster.postedText)
	}
}

func TestPostFallsBackToTextWhenSlackRejectsBlocks(t *testing.T) {
	poster := &fakePoster{blocksErr: errors.New("invalid_blocks")}

	err := Post(context.Background(), poster, nil, "fallback", quietLogger)

	if err != nil || poster.postedText != "fallback" {
		t.Errorf("Post() = %v, text = %q; want the plain-text fallback posted", err, poster.postedText)
	}
}

func TestPostReportsBothFailuresWhenNothingCanBePosted(t *testing.T) {
	poster := &fakePoster{blocksErr: errors.New("blocks failed"), textErr: errors.New("text failed")}

	err := Post(context.Background(), poster, nil, "fallback", quietLogger)

	if err == nil || !errors.Is(err, poster.blocksErr) || !errors.Is(err, poster.textErr) {
		t.Errorf("Post() error = %v, want both failures joined", err)
	}
}

func TestGatherLogsWhatEachSourceReturnedAtDebugLevel(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	gatherer := Gatherer{
		GitHub: fakeGitHub{data: activity.GitHub{
			Commits:   make([]activity.Commit, 3),
			PRsMerged: make([]activity.PullRequest, 2),
		}},
		Jira:       fakeJira{data: activity.Jira{Completed: make([]activity.Ticket, 4)}},
		Confluence: fakeConfluence{data: activity.Confluence{Updated: make([]activity.Page, 1)}},
		NotesPath:  filepath.Join(t.TempDir(), "missing.md"),
		Logger:     logger,
	}

	gatherer.Gather(context.Background(), time.Now())

	for _, want := range []string{
		"source=GitHub commits=3 prs_opened=0 prs_merged=2 reviews=0",
		"source=Jira completed=4 updated=0 in_progress=0",
		"source=Confluence created=0 updated=1",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("debug log missing %q:\n%s", want, logs.String())
		}
	}
}
