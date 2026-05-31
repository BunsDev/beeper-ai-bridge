package connector

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aidb"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

type Connector struct {
	Bridge          *bridgev2.Bridge
	Config          Config
	Store           *aidb.Store
	AppServiceToken string
	HomeserverURL   string

	providerRoutesRegistered bool
	providerConfigMu         sync.Mutex
}

var _ bridgev2.NetworkConnector = (*Connector)(nil)
var _ bridgev2.ConfigValidatingNetwork = (*Connector)(nil)

func (c *Connector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "AI Chats",
		NetworkURL:           "https://www.beeper.com/ai",
		NetworkIcon:          defaultAIAssistantAvatarMXC,
		NetworkID:            aiid.NetworkID,
		BeeperBridgeType:     aiid.BeeperBridgeType,
		DefaultPort:          29344,
		DefaultCommandPrefix: "!ai",
	}
}

func (c *Connector) Init(bridge *bridgev2.Bridge) {
	c.Config.ApplyDefaults()
	c.Bridge = bridge
	c.Store = aidb.NewStore(bridge.DB.Database, bridge.ID, dbutil.ZeroLogger(bridge.Log.With().Str("db_section", "ai").Logger()))
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
	return nil
}

func (c *Connector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 7
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
		if userDomain == "beeper.localtest.me" {
			return "http://ai-services.beeper" + defaultAIServicesProxyPath
		}
		if isBeeperAIServiceDomain(userDomain) {
			return "https://ai-services." + userDomain + defaultAIServicesProxyPath
		}
		if userDomain != "" {
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

func isBeeperAIServiceDomain(domain string) bool {
	switch domain {
	case "beeper.com", "beeper-staging.com", "beeper-dev.com":
		return true
	default:
		return false
	}
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
			trimmed, ok := strings.CutSuffix(localpart, "bot")
			if ok && trimmed != "" {
				return networkid.UserLoginID(trimmed)
			}
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
