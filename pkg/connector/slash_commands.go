package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
)

type aiSlashCommand struct {
	name string
	arg  string
}

type aiSlashCommandDefinition struct {
	name            string
	usage           string
	description     string
	argRequired     bool
	needsRoomConfig bool
	noticeErrors    bool
	run             func(*Client, context.Context, *bridgev2.Portal, RoomConfig, string, aiCommandResponder) error
}

type aiCommandResponder interface {
	Reply(ctx context.Context, text string) error
}

type aiCommandResponderFunc func(ctx context.Context, text string) error

func (fn aiCommandResponderFunc) Reply(ctx context.Context, text string) error {
	return fn(ctx, text)
}

func aiSlashCommandDefinitions() []aiSlashCommandDefinition {
	return []aiSlashCommandDefinition{
		{
			name:        "help",
			usage:       "/help [command]",
			description: "Show available AI Bridge commands.",
			run:         runHelpCommand,
		},
		{
			name:            "model",
			usage:           "/model [model]",
			description:     "Show or set the AI model for this room. Use provider/model for a specific provider.",
			needsRoomConfig: true,
			noticeErrors:    true,
			run:             runModelCommand,
		},
		{
			name:            "reasoning",
			usage:           "/reasoning [off|minimal|low|medium|high|xhigh]",
			description:     "Show or set the reasoning level for this room when the selected model supports it.",
			needsRoomConfig: true,
			noticeErrors:    true,
			run:             runReasoningCommand,
		},
		{
			name:            "system-prompt",
			usage:           "/system-prompt [prompt|clear]",
			description:     "Show, set, or clear this room's additional system prompt.",
			needsRoomConfig: true,
			run:             runSystemPromptCommand,
		},
		{
			name:            "compact",
			usage:           "/compact [instructions]",
			description:     "Manually compact this room's AI session context.",
			needsRoomConfig: true,
			noticeErrors:    true,
			run:             runCompactCommand,
		},
		{
			name:         "abort",
			usage:        "/abort",
			description:  "Abort the active AI response or compaction.",
			noticeErrors: true,
			run:          runAbortCommand,
		},
		{
			name:            "session",
			usage:           "/session",
			description:     "Show this room's AI session info and stats.",
			needsRoomConfig: true,
			noticeErrors:    true,
			run:             runSessionCommand,
		},
	}
}

func parseAISlashCommand(body string) (aiSlashCommand, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "/") {
		return aiSlashCommand{}, false
	}
	name, arg, _ := strings.Cut(strings.TrimPrefix(body, "/"), " ")
	name = strings.ToLower(strings.TrimSpace(name))
	arg = strings.TrimSpace(arg)
	if _, ok := aiSlashCommandByName(name); ok {
		return aiSlashCommand{name: name, arg: arg}, true
	}
	return aiSlashCommand{}, false
}

func (cl *Client) handleAISlashCommand(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, bool, error) {
	if msg == nil || msg.Content == nil {
		return nil, false, nil
	}
	cmd, ok := parseAISlashCommand(msg.Content.Body)
	if !ok {
		return nil, false, nil
	}
	if msg.Portal == nil {
		return nil, true, fmt.Errorf("missing portal for AI command")
	}
	def, _ := aiSlashCommandByName(cmd.name)
	responder := aiCommandResponderFunc(func(ctx context.Context, text string) error {
		return cl.sendCommandNotice(ctx, msg.Portal, text)
	})
	if def.argRequired && cmd.arg == "" {
		if err := responder.Reply(ctx, aiSlashCommandUsage(def)); err != nil {
			return nil, true, err
		}
		return cl.commandHandledResponse(msg, "usage"), true, nil
	}
	var roomConfig RoomConfig
	if def.needsRoomConfig {
		var err error
		roomConfig, _, err = cl.Main.ReadRoomConfig(ctx, msg.Portal.MXID)
		if err != nil {
			return nil, true, err
		}
	}
	if err := def.run(cl, ctx, msg.Portal, roomConfig, cmd.arg, responder); err != nil {
		if def.noticeErrors {
			cl.logAISlashCommandError(ctx, msg, cmd, err, "AI slash command rejected")
			if noticeErr := responder.Reply(ctx, err.Error()); noticeErr != nil {
				return nil, true, noticeErr
			}
			return cl.commandHandledResponse(msg, "rejected"), true, nil
		}
		return nil, true, err
	}
	return cl.commandHandledResponse(msg, cmd.name), true, nil
}

func (cl *Client) logAISlashCommandError(ctx context.Context, msg *bridgev2.MatrixMessage, cmd aiSlashCommand, err error, message string) {
	logCtx := zerolog.Ctx(ctx).With().
		Str("action", "ai_slash_command").
		Str("command", cmd.name).
		Bool("arg_present", cmd.arg != "")
	if cl != nil && cl.UserLogin != nil {
		logCtx = logCtx.Str("login_id", string(cl.UserLogin.ID))
	}
	log := logCtx.Logger()
	event := log.Error().Err(err)
	if msg != nil {
		if msg.Portal != nil {
			event = event.
				Str("portal_id", string(msg.Portal.ID)).
				Str("portal_receiver", string(msg.Portal.Receiver)).
				Str("portal_mxid", string(msg.Portal.MXID))
		}
		if msg.Event != nil {
			event = event.
				Str("event_id", string(msg.Event.ID)).
				Str("event_type", string(msg.Event.Type.Type)).
				Str("sender", string(msg.Event.Sender))
		}
	}
	event.Msg(message)
}

func aiSlashCommandByName(name string) (aiSlashCommandDefinition, bool) {
	for _, def := range aiSlashCommandDefinitions() {
		if def.name == name {
			return def, true
		}
	}
	return aiSlashCommandDefinition{}, false
}

func aiSlashCommandUsage(def aiSlashCommandDefinition) string {
	return "Usage: " + def.usage
}

func aiSlashCommandHelp(topic string) string {
	topic = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(topic)), "/")
	if topic != "" {
		if def, ok := aiSlashCommandByName(topic); ok {
			return fmt.Sprintf("%s\n\n%s", aiSlashCommandUsage(def), def.description)
		}
	}
	var text strings.Builder
	if topic != "" {
		fmt.Fprintf(&text, "Unknown command `/%s`.\n\n", topic)
	}
	text.WriteString("AI Bridge commands:")
	for _, def := range aiSlashCommandDefinitions() {
		fmt.Fprintf(&text, "\n- `%s` - %s", def.usage, def.description)
	}
	return text.String()
}

func runHelpCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, _ RoomConfig, arg string, responder aiCommandResponder) error {
	return responder.Reply(ctx, aiSlashCommandHelp(arg))
}
