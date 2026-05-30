package connector

import (
	"context"
	"fmt"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
)

type ProviderLoginClient struct {
	Main      *Connector
	UserLogin *bridgev2.UserLogin
	loggedIn  bool
}

var _ bridgev2.NetworkAPI = (*ProviderLoginClient)(nil)
var _ bridgev2.IdentifierResolvingNetworkAPI = (*ProviderLoginClient)(nil)
var _ bridgev2.ContactListingNetworkAPI = (*ProviderLoginClient)(nil)
var _ bridgev2.UserSearchingNetworkAPI = (*ProviderLoginClient)(nil)

func (cl *ProviderLoginClient) Connect(ctx context.Context) {
	_, cl.loggedIn = cl.provider()
	if cl.loggedIn {
		cl.sendBridgeState(status.StateConnected)
	} else {
		cl.sendBridgeState(status.StateLoggedOut)
	}
}

func (cl *ProviderLoginClient) Disconnect() {
	cl.loggedIn = false
}

func (cl *ProviderLoginClient) IsLoggedIn() bool {
	return cl.loggedIn
}

func (cl *ProviderLoginClient) LogoutRemote(ctx context.Context) {
	if cl != nil && cl.UserLogin != nil {
		if meta, ok := cl.UserLogin.Metadata.(*aiid.UserLoginMetadata); ok {
			meta.Provider = nil
			_ = cl.UserLogin.Save(ctx)
		}
	}
	cl.loggedIn = false
	cl.sendBridgeState(status.StateLoggedOut)
}

func (cl *ProviderLoginClient) GetUserID() networkid.UserID {
	return networkid.UserID("login:" + string(cl.mainLoginID()))
}

func (cl *ProviderLoginClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return userID == cl.GetUserID()
}

func (cl *ProviderLoginClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	return nil, fmt.Errorf("provider logins do not own AI rooms")
}

func (cl *ProviderLoginClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	return aiAssistantUserInfo(), nil
}

func (cl *ProviderLoginClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	supportsAIState := cl != nil && cl.Main != nil && cl.Main.aiRoomStateStore().canRead()
	return roomFeaturesForModel(ai.Model{}, supportsAIState)
}

func (cl *ProviderLoginClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	return nil, fmt.Errorf("provider logins do not handle AI room messages")
}

func (cl *ProviderLoginClient) GetContactList(ctx context.Context) ([]*bridgev2.ResolveIdentifierResponse, error) {
	provider, ok := cl.provider()
	if !ok {
		return nil, nil
	}
	return providerModelContacts(ctx, cl.bridge(), provider, ""), nil
}

func (cl *ProviderLoginClient) SearchUsers(ctx context.Context, query string) ([]*bridgev2.ResolveIdentifierResponse, error) {
	provider, ok := cl.provider()
	if !ok {
		return nil, nil
	}
	return providerModelContacts(ctx, cl.bridge(), provider, strings.TrimSpace(query)), nil
}

func (cl *ProviderLoginClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	provider, ok := cl.provider()
	if !ok {
		return nil, fmt.Errorf("provider login %s is not configured", cl.UserLogin.ID)
	}
	model, ok := resolveModelForProvider(provider, identifier)
	if !ok {
		return nil, fmt.Errorf("unknown AI model %s", identifier)
	}
	resp := modelContactWithGhost(ctx, cl.bridge(), provider, model)
	if !createChat {
		return resp, nil
	}
	parent, err := cl.mainClient(ctx)
	if err != nil {
		return nil, err
	}
	chat, err := parent.createModelChat(ctx, provider, model)
	if err != nil {
		return nil, err
	}
	resp.Chat = chat
	return resp, nil
}

func (cl *ProviderLoginClient) mainLoginID() networkid.UserLoginID {
	if cl != nil && cl.Main != nil && cl.UserLogin != nil {
		return cl.Main.defaultLoginID(cl.UserLogin.UserMXID)
	}
	return ""
}

func (cl *ProviderLoginClient) bridge() *bridgev2.Bridge {
	if cl == nil || cl.Main == nil {
		return nil
	}
	return cl.Main.Bridge
}

func (cl *ProviderLoginClient) provider() (aiid.ProviderConfig, bool) {
	if cl == nil || cl.Main == nil {
		return aiid.ProviderConfig{}, false
	}
	return cl.Main.providerFromLogin(cl.UserLogin)
}

func (cl *ProviderLoginClient) mainClient(ctx context.Context) (*Client, error) {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil {
		return nil, fmt.Errorf("provider login is not attached to a connector")
	}
	user := cl.UserLogin.User
	if user == nil && cl.Main.Bridge != nil {
		var err error
		user, err = cl.Main.Bridge.GetExistingUserByMXID(ctx, cl.UserLogin.UserMXID)
		if err != nil {
			return nil, err
		}
	}
	if user == nil {
		return nil, fmt.Errorf("AI Chats user %s is unavailable", cl.UserLogin.UserMXID)
	}
	return cl.Main.aiChatsClient(ctx, user)
}

func (cl *ProviderLoginClient) sendBridgeState(state status.BridgeStateEvent) {
	if cl != nil && cl.UserLogin != nil && cl.UserLogin.BridgeState != nil {
		provider, _ := cl.provider()
		cl.UserLogin.Log.Debug().
			Str("action", "ai_provider_bridge_state").
			Str("login_id", string(cl.UserLogin.ID)).
			Str("main_login_id", string(cl.mainLoginID())).
			Str("provider_id", provider.ID).
			Str("state_event", string(state)).
			Msg("Sending AI provider bridge state")
		cl.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: state})
	}
}
