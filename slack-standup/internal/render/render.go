// Package render formats gathered activity as a Slack Block Kit payload or
// as a plain-text fallback message.
package render

import (
	"fmt"
	"strings"
	"time"

	"standup/internal/activity"
	"standup/internal/workday"
)

const (
	noLimit = 0

	commitLimit      = 5
	reviewLimit      = 5
	jiraUpdatedLimit = 3
	jiraProgressMax  = 3
	pageUpdatedLimit = 3
	blocksNotesLimit = 5

	planPlaceholder = "_Add your planned work here_"
	noActivityText  = "No significant activity to report."
)

// Input is everything needed to render one standup.
//
// A nil source means it was unavailable (missing token, failed request) and
// is left out of the detailed breakdown.
type Input struct {
	Now        time.Time
	GitHub     *activity.GitHub
	Jira       *activity.Jira
	Confluence *activity.Confluence
	Notes      activity.Notes
	// AISummary replaces the detailed breakdown when it contains text.
	AISummary string
}

// section is a titled list of lines in the detailed breakdown.
type section struct {
	Title string
	Lines []string
}

// commitFormat differs between plain text and Slack mrkdwn.
type commitFormat func(activity.Commit) string

func (in Input) hasAISummary() bool {
	return strings.TrimSpace(in.AISummary) != ""
}

// header returns the date range and the label for the progress section.
func (in Input) header() (dateRange, period string) {
	const (
		dayFormat     = "Monday, January 02"
		dayYearFormat = "Monday, January 02, 2006"
		mondayPeriod  = "Since Friday"
		weekdayPeriod = "Yesterday's Progress"
	)

	if in.Now.Weekday() == time.Monday {
		lastWorkday := workday.Last(in.Now)
		dateRange = fmt.Sprintf("%s - %s", lastWorkday.Format(dayFormat), in.Now.Format(dayYearFormat))
		return dateRange, mondayPeriod
	}
	return in.Now.Format(dayYearFormat), weekdayPeriod
}

func (in Input) planLines() []string {
	if len(in.Notes.Plan) == 0 {
		return []string{planPlaceholder}
	}
	return in.Notes.Plan
}

// activitySections collects sections from every source that has data.
// notesLimit applies to the completed personal notes.
func (in Input) activitySections(formatCommit commitFormat, notesLimit int) []section {
	var sections []section

	if in.GitHub != nil {
		sections = append(sections, githubSections(*in.GitHub, formatCommit)...)
	}
	if in.Jira != nil {
		sections = append(sections, jiraSections(*in.Jira)...)
	}
	if in.Confluence != nil {
		sections = append(sections, confluenceSections(*in.Confluence)...)
	}
	sections = append(sections, newSection("Recent Activity Summary:", in.Notes.Completed, notesLimit, identity)...)

	return sections
}

func githubSections(data activity.GitHub, formatCommit commitFormat) []section {
	var sections []section
	sections = append(sections, newSection("GitHub Commits:", data.Commits, commitLimit, formatCommit)...)
	sections = append(sections, newSection("PRs Merged:", data.PRsMerged, noLimit, pullRequestLine)...)
	sections = append(sections, newSection("PRs Opened:", data.PRsOpened, noLimit, pullRequestLine)...)
	sections = append(sections, newSection("Code Reviews:", data.Reviews, reviewLimit, pullRequestLine)...)
	return sections
}

func jiraSections(data activity.Jira) []section {
	var sections []section
	sections = append(sections, newSection("Jira - Completed:", data.Completed, noLimit, ticketLine)...)
	sections = append(sections, newSection("Jira - Updated:", data.Updated, jiraUpdatedLimit, ticketStatusLine)...)
	sections = append(sections, newSection("Jira - In Progress:", data.InProgress, jiraProgressMax, ticketLine)...)
	return sections
}

func confluenceSections(data activity.Confluence) []section {
	var sections []section
	sections = append(sections, newSection("Confluence - Created:", data.Created, noLimit, pageLine)...)
	sections = append(sections, newSection("Confluence - Updated:", data.Updated, pageUpdatedLimit, pageLine)...)
	return sections
}

// newSection returns a single section, or none when items is empty.
// A limit of noLimit keeps every item.
func newSection[T any](title string, items []T, limit int, formatItem func(T) string) []section {
	if len(items) == 0 {
		return nil
	}
	if limit != noLimit && len(items) > limit {
		items = items[:limit]
	}

	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = formatItem(item)
	}
	return []section{{Title: title, Lines: lines}}
}

func identity(line string) string { return line }

func pullRequestLine(pr activity.PullRequest) string {
	return fmt.Sprintf("<%s|#%d: %s>", pr.URL, pr.Number, pr.Title)
}

func ticketLine(ticket activity.Ticket) string {
	return fmt.Sprintf("<%s|%s> %s", ticket.URL, ticket.Key, ticket.Summary)
}

func ticketStatusLine(ticket activity.Ticket) string {
	return fmt.Sprintf("<%s|%s> %s (%s)", ticket.URL, ticket.Key, ticket.Summary, ticket.Status)
}

func pageLine(page activity.Page) string {
	return fmt.Sprintf("<%s|%s> (%s)", page.URL, page.Title, page.Space)
}

func bulletLines(lines []string) []string {
	bullets := make([]string, len(lines))
	for i, line := range lines {
		bullets[i] = "• " + line
	}
	return bullets
}
