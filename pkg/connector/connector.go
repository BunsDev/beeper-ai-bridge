package connector

import (
	"context"
	"fmt"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aidb"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type Connector struct {
	Bridge          *bridgev2.Bridge
	Config          Config
	Store           *aidb.Store
	AppServiceToken string
}

var _ bridgev2.NetworkConnector = (*Connector)(nil)
var _ bridgev2.ConfigValidatingNetwork = (*Connector)(nil)

func (c *Connector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "AI",
		NetworkURL:           "https://beeper.com",
		NetworkID:            aiid.NetworkID,
		BeeperBridgeType:     aiid.BeeperBridgeType,
		DefaultPort:          29344,
		DefaultCommandPrefix: "!ai",
	}
}

func (c *Connector) Init(bridge *bridgev2.Bridge) {
	c.Config.ApplyDefaults()
	c.Bridge = bridge
	c.Store = aidb.NewStore(bridge.DB.Database, dbutil.ZeroLogger(bridge.Log.With().Str("db_section", "ai").Logger()))
}

func (c *Connector) Start(ctx context.Context) error {
	if _, ok := c.Bridge.Matrix.(bridgev2.MatrixConnectorWithBeeperStreams); !ok {
		return fmt.Errorf("AI bridge requires a Matrix connector with Beeper stream support")
	}
	if err := c.Store.Upgrade(ctx); err != nil {
		return bridgev2.DBUpgradeError{Err: err, Section: "ai"}
	}
	return nil
}

func (c *Connector) ValidateConfig() error {
	c.Config.ApplyDefaults()
	if c.Config.Tools.Enabled && len(c.Config.Tools.WorkspaceRoots) == 0 {
		return fmt.Errorf("network.tools.workspace_roots must be configured when tools are enabled")
	}
	if c.Config.DefaultProvider.BaseURL == "" {
		return fmt.Errorf("network.default_provider.base_url is required")
	}
	return nil
}

func (c *Connector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	meta := login.Metadata.(*aiid.UserLoginMetadata)
	ensureMetadataDefaults(meta, c.defaultProviderConfig())
	login.Client = &Client{
		Main:      c,
		UserLogin: login,
		loggedIn:  true,
	}
	return nil
}

func (c *Connector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 1
}

func (c *Connector) defaultProviderConfig() aiid.ProviderConfig {
	models := make([]ai.Model, 0, len(c.Config.DefaultProvider.Models))
	for _, model := range c.Config.DefaultProvider.Models {
		models = append(models, normalizeDefaultModel(model, c.Config.DefaultProvider.BaseURL))
	}
	return aiid.ProviderConfig{
		ID:           aiid.DefaultProvider,
		DisplayName:  "Beeper AI",
		API:          ai.ApiOpenAIResponses,
		Provider:     ai.Provider(aiid.DefaultProvider),
		BaseURL:      normalizeResponsesBaseURL(c.Config.DefaultProvider.BaseURL),
		DefaultModel: defaultModelID(models),
		Models:       models,
		Enabled:      true,
	}
}

func ensureMetadataDefaults(meta *aiid.UserLoginMetadata, defaultProvider aiid.ProviderConfig) {
	if meta.Providers == nil {
		meta.Providers = map[string]aiid.ProviderConfig{}
	}
	if _, ok := meta.Providers[defaultProvider.ID]; !ok && meta.SyntheticDefault {
		meta.Providers[defaultProvider.ID] = defaultProvider
	}
	if meta.DefaultProviderID == "" {
		meta.DefaultProviderID = defaultProvider.ID
	}
	if meta.DefaultModelID == "" {
		meta.DefaultModelID = defaultProvider.DefaultModel
	}
}

func defaultModelID(models []ai.Model) string {
	if len(models) == 0 {
		return "gpt-5"
	}
	return models[0].ID
}

func (c *Connector) EnsureDefaultLogin(ctx context.Context, user *bridgev2.User) (*bridgev2.UserLogin, error) {
	loginID := aiid.DefaultLoginID(user.MXID)
	if cached := c.Bridge.GetCachedUserLoginByID(loginID); cached != nil {
		return cached, nil
	}
	meta := &aiid.UserLoginMetadata{SyntheticDefault: true}
	ensureMetadataDefaults(meta, c.defaultProviderConfig())
	return user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: aiid.DefaultLoginName,
		Metadata:   meta,
	}, &bridgev2.NewLoginParams{})
}

func (c *Connector) ResolveLogin(ctx context.Context, user *bridgev2.User, requested networkid.UserLoginID) (*bridgev2.UserLogin, error) {
	if requested == "" {
		return c.EnsureDefaultLogin(ctx, user)
	}
	if cached := c.Bridge.GetCachedUserLoginByID(requested); cached != nil && cached.UserMXID == user.MXID {
		return cached, nil
	}
	return nil, fmt.Errorf("unknown or inaccessible login %s", requested)
}
