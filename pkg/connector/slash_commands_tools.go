package connector

import (
	"context"
	"fmt"
	"strings"

	"maunium.net/go/mautrix/bridgev2"

	"github.com/beeper/ai-bridge/pkg/aiid"
)

func runSearchModeCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, arg string, responder aiCommandResponder) error {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return responder.Reply(ctx, fmt.Sprintf("Current search mode is `%s`. Options: `off`, `beeper`, `native`.", roomSearchMode(roomConfig)))
	}
	mode := normalizedToolMode(arg, "")
	if mode != toolModeOff && mode != toolModeBeeper && mode != toolModeNative {
		return fmt.Errorf("search mode %q is invalid", arg)
	}
	roomConfig.SearchMode = mode
	if _, err := cl.writeAIRoomState(ctx, portal, aiid.RoomToolsType, toolModeStateContent(roomConfig)); err != nil {
		return err
	}
	cl.refreshRoomCapabilities(ctx, portal)
	return responder.Reply(ctx, fmt.Sprintf("Search mode set to `%s`.", mode))
}

func runFetchModeCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, arg string, responder aiCommandResponder) error {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return responder.Reply(ctx, fmt.Sprintf("Current fetch mode is `%s`. Options: `off`, `beeper`, `native`.", roomFetchMode(roomConfig)))
	}
	mode := normalizedToolMode(arg, "")
	if mode != toolModeOff && mode != toolModeBeeper && mode != toolModeNative {
		return fmt.Errorf("fetch mode %q is invalid", arg)
	}
	roomConfig.FetchMode = mode
	if _, err := cl.writeAIRoomState(ctx, portal, aiid.RoomToolsType, toolModeStateContent(roomConfig)); err != nil {
		return err
	}
	cl.refreshRoomCapabilities(ctx, portal)
	return responder.Reply(ctx, fmt.Sprintf("Fetch mode set to `%s`.", mode))
}
