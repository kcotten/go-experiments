package render

import (
	"fmt"
	"strings"

	"standup/internal/activity"
)

// Text renders the plain-text fallback message used when Block Kit posting fails.
func Text(in Input) string {
	dateRange, period := in.header()
	parts := []string{
		fmt.Sprintf("## Daily Standup - %s\n", dateRange),
		"### " + period,
	}

	if in.hasAISummary() {
		parts = append(parts, in.AISummary+"\n")
	} else {
		for _, sec := range in.activitySections(textCommitLine, noLimit) {
			parts = append(parts, "\n**"+sec.Title+"**")
			parts = append(parts, bulletLines(sec.Lines)...)
		}
	}

	parts = append(parts, "\n### Today's Plan")
	parts = append(parts, bulletLines(in.planLines())...)

	parts = append(parts, "\n### Blockers", "• None")

	return strings.Join(parts, "\n")
}

func textCommitLine(commit activity.Commit) string {
	return fmt.Sprintf("[%s] %s (%s)", commit.Repo, commit.Message, commit.SHA)
}
