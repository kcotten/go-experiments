// Package standup coordinates gathering activity from every source and
// posting the rendered result.
package standup

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
	"standup/internal/notes"
	"standup/internal/render"
	"standup/internal/summary"
	"standup/internal/workday"
)

type GitHubSource interface {
	Activity(ctx context.Context, lastWorkday time.Time) (activity.GitHub, error)
}

type JiraSource interface {
	Activity(ctx context.Context, lastWorkday time.Time) (activity.Jira, error)
}

type ConfluenceSource interface {
	Activity(ctx context.Context, lastWorkday time.Time) (activity.Confluence, error)
}

// Gatherer collects activity from all sources at once.
type Gatherer struct {
	GitHub     GitHubSource
	Jira       JiraSource
	Confluence ConfluenceSource
	NotesPath  string
	Logger     *slog.Logger
}

// Activity is everything gathered for one standup. A nil source was
// unavailable, for example because its token is not set.
type Activity struct {
	GitHub     *activity.GitHub
	Jira       *activity.Jira
	Confluence *activity.Confluence
	Notes      activity.Notes
}

// Gather fetches every source concurrently. A failing source never stops the
// others: it is logged, and whatever it did return is kept.
func (g Gatherer) Gather(ctx context.Context, now time.Time) Activity {
	lastWorkday := workday.Last(now)
	var gathered Activity
	var sources sync.WaitGroup

	// Each goroutine writes only its own field, so no locking is needed.
	sources.Add(3)
	go func() {
		defer sources.Done()
		gathered.GitHub = collect(g.Logger, "GitHub", func() (activity.GitHub, error) {
			return g.GitHub.Activity(ctx, lastWorkday)
		}, func(data activity.GitHub) []any {
			return []any{
				"commits", len(data.Commits),
				"prs_opened", len(data.PRsOpened),
				"prs_merged", len(data.PRsMerged),
				"reviews", len(data.Reviews),
			}
		})
	}()
	go func() {
		defer sources.Done()
		gathered.Jira = collect(g.Logger, "Jira", func() (activity.Jira, error) {
			return g.Jira.Activity(ctx, lastWorkday)
		}, func(data activity.Jira) []any {
			return []any{
				"completed", len(data.Completed),
				"updated", len(data.Updated),
				"in_progress", len(data.InProgress),
			}
		})
	}()
	go func() {
		defer sources.Done()
		gathered.Confluence = collect(g.Logger, "Confluence", func() (activity.Confluence, error) {
			return g.Confluence.Activity(ctx, lastWorkday)
		}, func(data activity.Confluence) []any {
			return []any{
				"created", len(data.Created),
				"updated", len(data.Updated),
			}
		})
	}()

	g.Logger.Info("Reading personal notes...")
	gathered.Notes = g.loadNotes(lastWorkday, now)

	sources.Wait()
	return gathered
}

func (g Gatherer) loadNotes(lastWorkday, today time.Time) activity.Notes {
	loaded, err := notes.Load(g.NotesPath, lastWorkday, today)
	if err != nil {
		g.Logger.Error("Could not read notes", "error", err)
	}
	g.Logger.Debug("Personal notes", "completed", len(loaded.Completed), "plan", len(loaded.Plan))
	return loaded
}

// collect runs one source. It returns nil when the source is unavailable for
// lack of a token, and otherwise whatever data came back, even alongside an
// error. counts describes the data for the debug log.
func collect[T any](logger *slog.Logger, name string, fetch func() (T, error), counts func(T) []any) *T {
	logger.Info("Fetching activity...", "source", name)

	data, err := fetch()
	switch {
	case errors.Is(err, httpx.ErrMissingToken):
		logger.Warn("Source unavailable", "source", name, "error", err)
		return nil
	case err != nil:
		logger.Error("Source reported errors; using what was collected", "source", name, "error", err)
	}

	logger.Debug("Source data", append([]any{"source", name}, counts(data)...)...)
	return &data
}

// SummaryData flattens the activity for the summary providers.
func (a Activity) SummaryData() summary.Data {
	data := summary.Data{Notes: a.Notes.Completed}
	if a.GitHub != nil {
		data.GitHub = *a.GitHub
	}
	if a.Jira != nil {
		data.Jira = *a.Jira
	}
	if a.Confluence != nil {
		data.Confluence = *a.Confluence
	}
	return data
}

// Input prepares the activity for rendering.
func (a Activity) Input(now time.Time, aiSummary string) render.Input {
	return render.Input{
		Now:        now,
		GitHub:     a.GitHub,
		Jira:       a.Jira,
		Confluence: a.Confluence,
		Notes:      a.Notes,
		AISummary:  aiSummary,
	}
}
