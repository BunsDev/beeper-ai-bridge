package connector

import (
	"context"
	"fmt"

	"maunium.net/go/mautrix/bridgev2"
)

func runAbortCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, _ RoomConfig, _ string, responder aiCommandResponder) error {
	h := cl.getActiveHarness(portal.PortalKey)
	if h == nil {
		return responder.Reply(ctx, "No active AI response or compaction to abort.")
	}
	result, err := h.Abort(ctx)
	if err != nil {
		return err
	}
	if active := cl.getActiveRun(portal.PortalKey); active != nil {
		active.failAll(ctx, cl, fmt.Errorf("AI run aborted"))
	}
	message := "Aborted active AI work."
	if cleared := len(result.ClearedSteer) + len(result.ClearedFollowUp); cleared > 0 {
		message = fmt.Sprintf("%s Cleared %d queued message(s).", message, cleared)
	}
	return responder.Reply(ctx, message)
}
