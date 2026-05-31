package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/util/shlex"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

var aiCommandHelpSection = commands.HelpSection{Name: "AI rooms", Order: 21}

type aiCommandProcessor interface {
	AddHandlers(...commands.CommandHandler)
}

type bridgeCommandResponder struct {
	ce *commands.Event
}

func (r bridgeCommandResponder) Reply(_ context.Context, text string) error {
	r.ce.Reply(text)
	return nil
}

func (c *Connector) registerAICommands() {
	if c == nil || c.Bridge == nil || c.Bridge.Commands == nil {
		return
	}
	processor, ok := c.Bridge.Commands.(aiCommandProcessor)
	if !ok {
		return
	}
	processor.AddHandlers(
		c.bridgeAICommand("model", "Show or set the AI model for this room.", "[model]"),
		c.bridgeAICommand("reasoning", "Show or set the reasoning level for this room.", "[off|minimal|low|medium|high|xhigh]"),
		c.bridgeAICommand("system-prompt", "Show, set, or clear this room's additional system prompt.", "[prompt|clear]"),
		c.bridgeAICommand("abort", "Abort the active AI response or compaction.", ""),
		c.bridgeAICommand("stop", "Stop the active AI response or compaction.", ""),
		c.bridgeProviderListCommand(),
		c.bridgeProviderCommand(),
		c.bridgeAICommand("ai-help", "Show available AI Bridge commands.", "[command]"),
	)
}

func (c *Connector) bridgeAICommand(name, description, args string) *commands.FullHandler {
	return &commands.FullHandler{
		Func: func(ce *commands.Event) {
			c.handleBridgeAICommand(ce)
		},
		Name: name,
		Help: commands.HelpMeta{
			Section:     aiCommandHelpSection,
			Description: description,
			Args:        args,
		},
		RequiresPortal: true,
		RequiresLogin:  true,
	}
}

func (c *Connector) handleBridgeAICommand(ce *commands.Event) {
	if ce == nil {
		return
	}
	defName := canonicalAICommandName(ce.Command)
	def, ok := aiSlashCommandByName(defName)
	if !ok {
		ce.Reply("Unknown AI command.")
		return
	}
	cl, err := c.clientForBridgeAICommand(ce)
	if err != nil {
		ce.Reply(err.Error())
		return
	}
	arg := strings.TrimSpace(ce.RawArgs)
	if def.argRequired && arg == "" {
		ce.Reply(aiSlashCommandUsage(def))
		return
	}
	var roomConfig RoomConfig
	if def.needsRoomConfig {
		roomConfig, _, err = c.ReadRoomConfig(ce.Ctx, ce.Portal.MXID)
		if err != nil {
			ce.Log.Err(err).Msg("Failed to read AI room state for command")
			ce.Reply("Failed to read AI room settings.")
			return
		}
	}
	err = def.run(cl, ce.Ctx, ce.Portal, roomConfig, arg, bridgeCommandResponder{ce: ce})
	if err != nil {
		logEvt := ce.Log.Err(err).
			Str("action", "ai_bridge_command").
			Str("command", def.name).
			Bool("arg_present", arg != "")
		if ce.Portal != nil {
			logEvt = logEvt.
				Str("portal_id", string(ce.Portal.ID)).
				Str("portal_receiver", string(ce.Portal.Receiver)).
				Str("portal_mxid", string(ce.Portal.MXID))
		}
		if def.noticeErrors {
			logEvt.Msg("AI bridge command rejected")
			ce.Reply(err.Error())
		} else {
			logEvt.Msg("AI bridge command failed")
			ce.Reply("AI command failed: %v", err)
		}
	}
}

func (c *Connector) bridgeProviderListCommand() *commands.FullHandler {
	return &commands.FullHandler{
		Func: func(ce *commands.Event) {
			c.handleBridgeProvidersCommand(ce)
		},
		Name: "providers",
		Help: commands.HelpMeta{
			Section:     aiCommandHelpSection,
			Description: "List configured AI providers.",
			Args:        "",
		},
		RequiresLoginPermission: true,
	}
}

func (c *Connector) bridgeProviderCommand() *commands.FullHandler {
	return &commands.FullHandler{
		Func: func(ce *commands.Event) {
			c.handleBridgeProviderCommand(ce)
		},
		Name: "provider",
		Help: commands.HelpMeta{
			Section:     aiCommandHelpSection,
			Description: "Show, add, update, or delete an AI provider.",
			Args:        "<show|add|update|delete> ...",
		},
		RequiresLoginPermission: true,
	}
}

func (c *Connector) handleBridgeProvidersCommand(ce *commands.Event) {
	login, err := c.loginForBridgeProviderCommand(ce)
	if err != nil {
		ce.Reply(err.Error())
		return
	}
	ce.Reply(providerListText(sortedProviderResponses(c.providersForLogin(login))))
}

func (c *Connector) handleBridgeProviderCommand(ce *commands.Event) {
	fields, err := parseBridgeProviderArgs(ce.RawArgs)
	if err != nil {
		if providerCommandMayContainSecret(ce.RawArgs) {
			ce.Redact()
		}
		ce.Reply("Invalid provider command syntax: %v", err)
		return
	}
	if len(fields) == 0 {
		ce.Reply("Usage: `$cmdprefix provider <show|add|update|delete> ...`")
		return
	}
	switch fields[0] {
	case "show":
		c.handleBridgeProviderShow(ce, fields[1:])
	case "add", "update":
		c.handleBridgeProviderUpsert(ce, fields[0], fields[1:])
	case "delete":
		c.handleBridgeProviderDelete(ce, fields[1:])
	default:
		ce.Reply("Usage: `$cmdprefix provider <show|add|update|delete> ...`")
	}
}

func parseBridgeProviderArgs(raw string) ([]string, error) {
	return shlex.Split(raw)
}

func providerCommandMayContainSecret(raw string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(raw)))
	return len(fields) > 0 && (fields[0] == "add" || fields[0] == "update")
}

func (c *Connector) handleBridgeProviderShow(ce *commands.Event, fields []string) {
	if len(fields) != 1 {
		ce.Reply("Usage: `$cmdprefix provider show <id>`")
		return
	}
	login, err := c.loginForBridgeProviderCommand(ce)
	if err != nil {
		ce.Reply(err.Error())
		return
	}
	provider, ok := c.providerForLogin(login, fields[0])
	if !ok {
		ce.Reply("Provider `%s` not found.", fields[0])
		return
	}
	ce.Reply(providerText(providerResponse(provider)))
}

func (c *Connector) handleBridgeProviderUpsert(ce *commands.Event, action string, fields []string) {
	if len(fields) < 4 || len(fields) > 5 {
		ce.Reply("Usage: `$cmdprefix provider %s <id> <api> <base_url> <api_key> [default_model]`", action)
		return
	}
	ce.Redact()
	input := ProviderInput{
		ID:           fields[0],
		API:          ai.Api(fields[1]),
		BaseURL:      fields[2],
		APIKey:       fields[3],
		DefaultModel: "",
	}
	if len(fields) == 5 {
		input.DefaultModel = fields[4]
	}
	provider, err := c.VerifyProviderConfig(ce.Ctx, input)
	if err != nil {
		ce.Reply("Provider rejected: %v", err)
		return
	}
	login, err := c.loginForBridgeProviderCommand(ce)
	if err != nil {
		ce.Reply(err.Error())
		return
	}
	if err = c.SaveProviderConfig(ce.Ctx, login, provider); err != nil {
		ce.Reply("Provider save failed: %v", err)
		return
	}
	if action == "add" {
		ce.Reply("Provider `%s` added with default model `%s`.", provider.ID, provider.DefaultModel)
	} else {
		ce.Reply("Provider `%s` updated with default model `%s`.", provider.ID, provider.DefaultModel)
	}
}

func (c *Connector) handleBridgeProviderDelete(ce *commands.Event, fields []string) {
	if len(fields) != 1 {
		ce.Reply("Usage: `$cmdprefix provider delete <id>`")
		return
	}
	login, err := c.loginForBridgeProviderCommand(ce)
	if err != nil {
		ce.Reply(err.Error())
		return
	}
	if err := c.DeleteProvider(ce.Ctx, login, fields[0]); err != nil {
		ce.Reply("Provider delete failed: %v", err)
		return
	}
	ce.Reply("Provider `%s` deleted.", fields[0])
}

func providerListText(providers []ProviderResponse) string {
	if len(providers) == 0 {
		return "No AI providers are configured."
	}
	var text strings.Builder
	text.WriteString("AI providers:")
	for _, provider := range providers {
		fmt.Fprintf(&text, "\n- `%s` - %s", provider.ID, provider.DisplayName)
		if provider.DefaultModel != "" {
			fmt.Fprintf(&text, " (default `%s`)", provider.DefaultModel)
		}
		if provider.ReadOnly {
			text.WriteString(" [read-only]")
		}
	}
	return text.String()
}

func providerText(provider ProviderResponse) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Provider `%s`\n", provider.ID)
	fmt.Fprintf(&text, "- Name: `%s`\n", provider.DisplayName)
	fmt.Fprintf(&text, "- API: `%s`\n", provider.API)
	fmt.Fprintf(&text, "- Route: `%s`\n", provider.Provider)
	fmt.Fprintf(&text, "- Base URL: `%s`\n", provider.BaseURL)
	if provider.DefaultModel != "" {
		fmt.Fprintf(&text, "- Default model: `%s`\n", provider.DefaultModel)
	}
	fmt.Fprintf(&text, "- Models: `%d`", len(provider.Models))
	if provider.ReadOnly {
		text.WriteString("\n- Read-only: `true`")
	}
	return text.String()
}

func (c *Connector) loginForBridgeProviderCommand(ce *commands.Event) (*bridgev2.UserLogin, error) {
	if ce != nil && ce.Portal != nil {
		return c.loginForBridgePortalCommand(ce)
	}
	return c.EnsureAIChatsLogin(ce.Ctx, ce.User)
}

func (c *Connector) loginForBridgePortalCommand(ce *commands.Event) (*bridgev2.UserLogin, error) {
	login, _, err := ce.Portal.FindPreferredLogin(ce.Ctx, ce.User, false)
	if errors.Is(err, bridgev2.ErrNotLoggedIn) {
		return nil, errors.New("You're not logged in for this AI room.")
	} else if err != nil {
		ce.Log.Err(err).Msg("Failed to find preferred AI login for command")
		return nil, errors.New("Failed to find the AI login for this room.")
	}
	return login, nil
}

func (c *Connector) clientForBridgeAICommand(ce *commands.Event) (*Client, error) {
	if ce.Portal == nil {
		return nil, errors.New("This command can only be used in AI portal rooms.")
	}
	login, err := c.loginForBridgePortalCommand(ce)
	if err != nil {
		return nil, err
	}
	cl, ok := login.Client.(*Client)
	if !ok {
		return nil, fmt.Errorf("login %s is not loaded as an AI client", login.ID)
	}
	return cl, nil
}
