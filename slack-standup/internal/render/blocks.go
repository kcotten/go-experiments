package render

import (
	"fmt"
	"strings"

	"standup/internal/activity"
)

// Block is the subset of Slack Block Kit used by the standup message.
type Block struct {
	Type string     `json:"type"`
	Text *BlockText `json:"text,omitempty"`
}

type BlockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Blocks renders the standup as Slack Block Kit.
func Blocks(in Input) []Block {
	dateRange, period := in.header()

	progress := in.AISummary
	if !in.hasAISummary() {
		progress = in.progressText()
	}
	plan := strings.Join(bulletLines(in.planLines()), "\n")

	return []Block{
		{Type: "header", Text: &BlockText{Type: "plain_text", Text: "Daily Standup - " + dateRange}},
		mrkdwnSection(fmt.Sprintf("*%s*\n%s", period, progress)),
		{Type: "divider"},
		mrkdwnSection("*Today's Plan*\n" + plan),
		mrkdwnSection("*Blockers*\n• None"),
	}
}

func mrkdwnSection(text string) Block {
	return Block{Type: "section", Text: &BlockText{Type: "mrkdwn", Text: text}}
}

// progressText is the detailed Slack-formatted breakdown used without an AI summary.
func (in Input) progressText() string {
	sections := in.activitySections(blocksCommitLine, blocksNotesLimit)
	if len(sections) == 0 {
		return noActivityText
	}

	rendered := make([]string, len(sections))
	for i, sec := range sections {
		rendered[i] = "*" + sec.Title + "*\n" + strings.Join(bulletLines(sec.Lines), "\n")
	}
	return strings.Join(rendered, "\n\n")
}

func blocksCommitLine(commit activity.Commit) string {
	return fmt.Sprintf("*%s*: %s (`%s`)", commit.Repo, commit.Message, commit.SHA)
}
