package connector

import (
	"context"
	"fmt"
	"strings"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

const beeperUsageLimitMessage = "This message exceeds the AI usage limits in your plan. Your limits will reset in %s. You can see details by typing `/limits`"

func (cl *Client) withProviderVisibleError(ctx context.Context, provider aiid.ProviderConfig, message ai.Message) ai.Message {
	if message.StopReason != ai.StopReasonError || strings.TrimSpace(message.ErrorMessage) == "" {
		return message
	}
	message.ErrorMessage = cl.visibleProviderErrorMessage(ctx, provider, message.ErrorMessage)
	return message
}

func (cl *Client) visibleProviderErrorMessage(ctx context.Context, provider aiid.ProviderConfig, message string) string {
	if provider.ID != aiid.DefaultProvider || !isBeeperRateLimitError(message) {
		return message
	}
	now := time.Now()
	resetAt, ok := cl.beeperUsageLimitResetAt(ctx, now)
	if !ok {
		return fmt.Sprintf(beeperUsageLimitMessage, "an upcoming plan window")
	}
	return fmt.Sprintf(beeperUsageLimitMessage, formatResetIn(resetAt, now))
}

func (cl *Client) beeperUsageLimitResetAt(ctx context.Context, now time.Time) (time.Time, bool) {
	limits, err := cl.fetchAIServicesLimits(ctx)
	if err != nil {
		return time.Time{}, false
	}
	var resetAt time.Time
	for _, window := range []aiServicesLimitWindow{
		limits.Windows.LLM.Day,
		limits.Windows.LLM.Week,
		limits.Windows.LLM.Month,
	} {
		if window.Limit == -1 || window.Remaining > 0 || window.ResetAtMS <= 0 {
			continue
		}
		candidate := time.UnixMilli(window.ResetAtMS)
		if candidate.After(resetAt) {
			resetAt = candidate
		}
	}
	if resetAt.IsZero() {
		return time.Time{}, false
	}
	return resetAt, resetAt.After(now)
}

func isBeeperRateLimitError(message string) bool {
	lower := strings.ToLower(message)
	return isAIUsageLimitError(lower) ||
		strings.Contains(lower, "web tools limit exceeded") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "(429)")
}
