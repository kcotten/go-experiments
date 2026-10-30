package summary

import (
	"fmt"
	"strings"

	"standup/internal/activity"
)

const (
	defaultPromptStyle = "professional"

	promptCommitLimit = 5
	promptTicketLimit = 3
	promptNotesLimit  = 3
)

// promptStyle wraps the activity list: an introduction before it and an
// instruction after it.
type promptStyle struct {
	intro       string
	instruction string
}

var promptStyles = map[string]promptStyle{
	"casual": {
		intro:       "Here's what I got done recently:",
		instruction: "Please write a brief, casual daily standup summary (2-3 sentences) highlighting the key accomplishments. Keep it friendly and conversational.",
	},
	"detailed": {
		intro:       "Work Activity Summary:",
		instruction: "Please create a detailed daily standup summary that categorizes the work and provides context about impact and progress. Include technical details where relevant.",
	},
	defaultPromptStyle: {
		intro:       "Recent Work Activity:",
		instruction: "Please create a concise, professional daily standup summary (2-3 sentences) focusing on key accomplishments and their business impact.",
	},
}

// buildPrompt asks for a summary of data in the named style, falling back to
// the professional style for an unknown name.
func buildPrompt(data Data, styleName string) string {
	style, known := promptStyles[styleName]
	if !known {
		style = promptStyles[defaultPromptStyle]
	}

	activityList := strings.Join(activityLines(data), "\n")
	return fmt.Sprintf("%s\n\n%s\n\n%s", style.intro, activityList, style.instruction)
}

// activityLines lists the activity as plain text. Reviews are reduced to a
// count, and in-progress tickets and Confluence pages are left out.
func activityLines(data Data) []string {
	var lines []string

	lines = appendGroup(lines, "GitHub Commits:", formatAll(data.GitHub.Commits, promptCommitLimit, commitLine))
	lines = appendGroup(lines, "Pull Requests Merged:", formatAll(data.GitHub.PRsMerged, noLimit, pullRequestLine))
	lines = appendGroup(lines, "Pull Requests Opened:", formatAll(data.GitHub.PRsOpened, noLimit, pullRequestLine))
	if len(data.GitHub.Reviews) > 0 {
		lines = append(lines, fmt.Sprintf("Code Reviews: %d PRs reviewed", len(data.GitHub.Reviews)))
	}
	lines = appendGroup(lines, "Jira Tickets Completed:", formatAll(data.Jira.Completed, noLimit, ticketLine))
	lines = appendGroup(lines, "Jira Tickets Updated:", formatAll(data.Jira.Updated, promptTicketLimit, ticketLine))
	lines = appendGroup(lines, "Other Activities:", formatAll(data.Notes, promptNotesLimit, func(note string) string { return note }))

	return lines
}

const noLimit = 0

// appendGroup adds a titled bullet list, or nothing when there are no items.
func appendGroup(lines []string, title string, items []string) []string {
	if len(items) == 0 {
		return lines
	}
	lines = append(lines, title)
	for _, item := range items {
		lines = append(lines, "- "+item)
	}
	return lines
}

func formatAll[T any](items []T, limit int, format func(T) string) []string {
	if limit != noLimit && len(items) > limit {
		items = items[:limit]
	}
	formatted := make([]string, len(items))
	for i, item := range items {
		formatted[i] = format(item)
	}
	return formatted
}

func commitLine(commit activity.Commit) string {
	return fmt.Sprintf("%s (%s)", commit.Message, commit.Repo)
}

func pullRequestLine(pr activity.PullRequest) string {
	return fmt.Sprintf("%s (%s)", pr.Title, pr.Repo)
}

func ticketLine(ticket activity.Ticket) string {
	return fmt.Sprintf("%s: %s", ticket.Key, ticket.Summary)
}
