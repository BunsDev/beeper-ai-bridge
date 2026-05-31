package connector

import (
	"context"
	"fmt"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
)

func runApproveCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, _ RoomConfig, arg string, responder aiCommandResponder) error {
	approvalID, rawChoice, ok := strings.Cut(strings.TrimSpace(arg), " ")
	if !ok || strings.TrimSpace(approvalID) == "" || strings.TrimSpace(rawChoice) == "" {
		return fmt.Errorf("Usage: /approve <approval-id> <approve|always|deny>")
	}
	response, ok := approvalResponseFromCommand(strings.TrimSpace(approvalID), strings.TrimSpace(rawChoice))
	if !ok {
		return fmt.Errorf("unknown approval choice %q", strings.TrimSpace(rawChoice))
	}
	active := cl.getActiveRun(portal.PortalKey)
	if active == nil || !active.resolveApproval(response) {
		return fmt.Errorf("approval %s is not pending", approvalID)
	}
	switch {
	case response.Always:
		return responder.Reply(ctx, "Approval saved. Continuing.")
	case response.Approved:
		return responder.Reply(ctx, "Approved. Continuing.")
	default:
		return responder.Reply(ctx, "Denied. Continuing without the requested access.")
	}
}

func runResetApprovalsCommand(cl *Client, ctx context.Context, _ *bridgev2.Portal, _ RoomConfig, arg string, responder aiCommandResponder) error {
	if strings.TrimSpace(arg) != "" {
		return fmt.Errorf("Usage: /reset-approvals")
	}
	if err := cl.resetApprovalDecisions(ctx); err != nil {
		return err
	}
	return responder.Reply(ctx, "Saved AI approvals reset.")
}
