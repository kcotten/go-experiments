package standup

import (
	"context"
	"errors"
	"log/slog"

	"standup/internal/render"
	"standup/internal/slack"
)

// Poster sends a standup to Slack.
type Poster interface {
	PostBlocks(ctx context.Context, blocks []render.Block) error
	PostText(ctx context.Context, text string) error
}

// Post sends the rich blocks message, falling back to plain text if Slack
// rejects it.
func Post(ctx context.Context, poster Poster, blocks []render.Block, text string, logger *slog.Logger) error {
	blocksErr := poster.PostBlocks(ctx, blocks)
	if blocksErr == nil {
		logger.Info("Posted to Slack with rich formatting")
		return nil
	}
	if errors.Is(blocksErr, slack.ErrNoWebhook) {
		return blocksErr
	}

	logger.Warn("Posting blocks to Slack failed; retrying as plain text", "error", blocksErr)
	if err := poster.PostText(ctx, text); err != nil {
		return errors.Join(blocksErr, err)
	}
	logger.Info("Posted to Slack with basic formatting")
	return nil
}
