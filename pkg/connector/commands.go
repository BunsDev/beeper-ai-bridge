package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func (c *Connector) commandAddProvider() commands.CommandHandler {
	return &commands.FullHandler{
		Func: func(ce *commands.Event) {
			if len(ce.Args) < 5 {
				ce.Reply("Usage: `$cmdprefix add-provider <login id|default> <provider id> <base url> <api key> <default model> [models...]`")
				return
			}
			loginID := networkid.UserLoginID(ce.Args[0])
			if ce.Args[0] == "default" {
				loginID = c.defaultLoginID(ce.User.MXID)
			}
			provider, err := buildProviderFromCommandArgs(ce.Args[1:])
			if err != nil {
				ce.Reply("Invalid provider: %v", err)
				return
			}
			login, err := c.ResolveLogin(ce.Ctx, ce.User, loginID)
			if err != nil {
				ce.Reply("Failed to resolve login: %v", err)
				return
			}
			if err = c.AddProviderToLogin(ce.Ctx, login, provider); err != nil {
				ce.Reply("Failed to add provider: %v", err)
				return
			}
			ce.Reply("Provider `%s` added to login `%s`.", provider.ID, login.ID)
		},
		Name:                    "add-provider",
		Aliases:                 []string{"add_provider"},
		RequiresLoginPermission: true,
		Help: commands.HelpMeta{
			Section:     commands.HelpSectionAuth,
			Description: "Add an OpenAI-compatible provider to an existing AI login.",
			Args:        "<login id|default> <provider id> <base url> <api key> <default model> [models...]",
		},
	}
}

func buildProviderFromCommandArgs(args []string) (aiid.ProviderConfig, error) {
	providerID := strings.TrimSpace(args[0])
	baseURL := normalizeResponsesBaseURL(strings.TrimSpace(args[1]))
	apiKey := strings.TrimSpace(args[2])
	defaultModel := strings.TrimSpace(args[3])
	if providerID == "" || baseURL == "" || apiKey == "" || defaultModel == "" {
		return aiid.ProviderConfig{}, fmt.Errorf("provider id, base url, api key and default model are required")
	}
	return customProviderConfig(providerID, providerID, baseURL, apiKey, defaultModel, strings.Join(args[4:], ",")), nil
}

func (c *Connector) AddProviderToLogin(ctx context.Context, login *bridgev2.UserLogin, provider aiid.ProviderConfig) error {
	if provider.ID == "" {
		return fmt.Errorf("provider id is required")
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok {
		return fmt.Errorf("unexpected login metadata type %T", login.Metadata)
	}
	ensureMetadataDefaults(meta, c.defaultProviderConfig(), c.configuredProviders())
	meta.Providers[provider.ID] = provider
	if meta.DefaultProviderID == "" || meta.DefaultProviderID == aiid.DefaultProvider && !meta.SyntheticDefault {
		meta.DefaultProviderID = provider.ID
		meta.DefaultModelID = provider.DefaultModel
	}
	return login.Save(ctx)
}
