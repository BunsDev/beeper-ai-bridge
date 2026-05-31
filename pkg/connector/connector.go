package connector

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aidb"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

type Connector struct {
	Bridge          *bridgev2.Bridge
	Config          Config
	Store           *aidb.Store
	AppServiceToken string
	HomeserverURL   string

	providerRoutesRegistered bool
}

var _ bridgev2.NetworkConnector = (*Connector)(nil)
var _ bridgev2.ConfigValidatingNetwork = (*Connector)(nil)

func (c *Connector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "AI Chats",
		NetworkURL:           "https://www.beeper.com/ai",
		NetworkIcon:          "mxc://beeper.com/51a668657dd9e0132cc823ad9402c6c2d0fc3321",
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
	c.registerAICommands()
}

func (c *Connector) Start(ctx context.Context) error {
	if _, ok := c.Bridge.Matrix.(bridgev2.MatrixConnectorWithBeeperStreams); !ok {
		return fmt.Errorf("AI bridge requires a Matrix connector with Beeper stream support")
	}
	if err := c.Store.Upgrade(ctx); err != nil {
		return bridgev2.DBUpgradeError{Err: err, Section: "ai"}
	}
	c.registerProviderRoutes()
	if err := c.ensureAIChatsLoginsForPersistedUsers(ctx); err != nil {
		c.Bridge.Log.Warn().Err(err).Msg("Failed to ensure AI logins for persisted users")
	}
	return nil
}

func (c *Connector) ValidateConfig() error {
	c.Config.ApplyDefaults()
	return nil
}

func (c *Connector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	if err := c.ensureAIChatsMetadata(ctx, login); err != nil {
		return err
	}
	client := &Client{
		Main:      c,
		UserLogin: login,
		loggedIn:  true,
	}
	login.Client = client
	client.failPersistedActiveStreams(ctx)
	return nil
}

func (c *Connector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 4
}

func (c *Connector) defaultProviderConfig(userMXID id.UserID) aiid.ProviderConfig {
	baseURL := c.defaultAIServicesOpenAIProxyBaseURL(userMXID)
	return aiid.ProviderConfig{
		ID:           aiid.DefaultProvider,
		DisplayName:  "Beeper AI",
		API:          ai.ApiOpenAIResponses,
		Provider:     ai.ProviderOpenAI,
		BaseURL:      normalizeResponsesBaseURL(baseURL),
		DefaultModel: defaultBeeperAIModel,
	}
}

func (c *Connector) defaultAIServicesOpenAIProxyBaseURL(userMXID id.UserID) string {
	userDomain := userMXID.Homeserver()
	bridgeHost := c.homeserverAddressHost()
	if bridgeHost == "megahungry-proxy.megahungry" {
		if userDomain != "" && userDomain != "beeper.localtest.me" {
			return ""
		}
		return "http://ai-services.beeper" + defaultAIServicesProxyPath
	}
	domain := homeserverServiceDomain(bridgeHost)
	if domain != "" {
		if userDomain != "" && userDomain != domain {
			return ""
		}
		return "https://ai-services." + domain + defaultAIServicesProxyPath
	}
	if userDomain == "" {
		return ""
	}
	return "https://ai-services." + userDomain + defaultAIServicesProxyPath
}

func homeserverServiceDomain(host string) string {
	host = strings.TrimPrefix(host, "matrix.")
	return host
}

func (c *Connector) homeserverAddressHost() string {
	if c == nil {
		return ""
	}
	parsed, err := url.Parse(c.HomeserverURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func (c *Connector) defaultLoginID(mxid id.UserID) networkid.UserLoginID {
	if loginID := c.bridgeDefaultLoginID(); loginID != "" {
		return loginID
	}
	return c.perUserDefaultLoginID(mxid)
}

func (c *Connector) perUserDefaultLoginID(mxid id.UserID) networkid.UserLoginID {
	return aiid.DefaultLoginID(mxid)
}

func (c *Connector) bridgeDefaultLoginID() networkid.UserLoginID {
	if c.Bridge != nil && c.Bridge.Bot != nil {
		if localpart := c.Bridge.Bot.GetMXID().Localpart(); localpart != "" {
			return networkid.UserLoginID(strings.TrimSuffix(localpart, "bot"))
		}
	}
	return ""
}

func (c *Connector) ResolveLogin(ctx context.Context, user *bridgev2.User, requested networkid.UserLoginID) (*bridgev2.UserLogin, error) {
	if requested == "" {
		return nil, fmt.Errorf("login ID is required")
	}
	if cached := c.Bridge.GetCachedUserLoginByID(requested); cached != nil && cached.UserMXID == user.MXID {
		return cached, nil
	}
	return nil, fmt.Errorf("unknown or inaccessible login %s", requested)
}
