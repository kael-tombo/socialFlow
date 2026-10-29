package waitlist

import (
	"context"
	"errors"
	"time"

	"github.com/openpost/backend/internal/discordwebhook"
)

func (s *Service) notifyDiscord(ctx context.Context, item entry) error {
	if s.webhookURL == "" {
		return errors.New("waitlist Discord webhook is not configured")
	}
	return discordwebhook.Send(ctx, s.client, s.webhookURL, map[string]any{
		"username":         "SocialFlow.ai waitlist",
		"allowed_mentions": map[string]any{"parse": []string{}},
		"embeds": []map[string]any{{
			"title":       "New Hosted waitlist signup",
			"description": item.Email,
			"timestamp":   item.CreatedAt.Format(time.RFC3339),
		}},
	})
}
