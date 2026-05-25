package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
)

type ProviderRemoteClient struct {
	Main      *Connector
	UserLogin *bridgev2.UserLogin
	loggedIn  bool
}

var _ bridgev2.NetworkAPI = (*ProviderRemoteClient)(nil)
var _ bridgev2.IdentifierResolvingNetworkAPI = (*ProviderRemoteClient)(nil)
var _ bridgev2.ContactListingNetworkAPI = (*ProviderRemoteClient)(nil)
var _ bridgev2.UserSearchingNetworkAPI = (*ProviderRemoteClient)(nil)

func (cl *ProviderRemoteClient) Connect(ctx context.Context) {
	cl.loggedIn = cl.providerAvailable(ctx)
	if cl.loggedIn {
		cl.sendBridgeState(status.StateConnected)
	} else {
		cl.sendBridgeState(status.StateLoggedOut)
	}
}

func (cl *ProviderRemoteClient) Disconnect() {
	cl.loggedIn = false
}

func (cl *ProviderRemoteClient) IsLoggedIn() bool {
	return cl.loggedIn
}

func (cl *ProviderRemoteClient) LogoutRemote(ctx context.Context) {
	parent, providerID, err := cl.parentLogin(ctx)
	if err == nil {
		if meta, ok := parent.Metadata.(*aiid.UserLoginMetadata); ok && meta.Providers != nil {
			delete(meta.Providers, providerID)
			if meta.DefaultProviderID == providerID {
				meta.DefaultProviderID = ""
				meta.DefaultModelID = ""
				ensureMetadataDefaults(meta, cl.Main.defaultProviderConfig(), cl.Main.configuredProviders())
			}
			_ = parent.Save(ctx)
		}
	}
	cl.loggedIn = false
	cl.sendBridgeState(status.StateLoggedOut)
}

func (cl *ProviderRemoteClient) GetUserID() networkid.UserID {
	return networkid.UserID("login:" + string(cl.UserLogin.ID))
}

func (cl *ProviderRemoteClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return userID == cl.GetUserID()
}

func (cl *ProviderRemoteClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	parent, _, err := cl.parentClient(ctx)
	if err != nil {
		return nil, err
	}
	return parent.GetChatInfo(ctx, portal)
}

func (cl *ProviderRemoteClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	parent, _, err := cl.parentClient(ctx)
	if err != nil {
		return nil, err
	}
	return parent.GetUserInfo(ctx, ghost)
}

func (cl *ProviderRemoteClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	parent, _, err := cl.parentClient(ctx)
	if err != nil {
		return (&Client{}).GetCapabilities(ctx, portal)
	}
	return parent.GetCapabilities(ctx, portal)
}

func (cl *ProviderRemoteClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	parent, _, err := cl.parentClient(ctx)
	if err != nil {
		return nil, err
	}
	return parent.HandleMatrixMessage(ctx, msg)
}

func (cl *ProviderRemoteClient) GetContactList(ctx context.Context) ([]*bridgev2.ResolveIdentifierResponse, error) {
	provider, ok, err := cl.provider(ctx)
	if err != nil || !ok {
		return nil, err
	}
	return providerModelContacts(provider, ""), nil
}

func (cl *ProviderRemoteClient) SearchUsers(ctx context.Context, query string) ([]*bridgev2.ResolveIdentifierResponse, error) {
	provider, ok, err := cl.provider(ctx)
	if err != nil || !ok {
		return nil, err
	}
	return providerModelContacts(provider, strings.ToLower(strings.TrimSpace(query))), nil
}

func (cl *ProviderRemoteClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	parent, providerID, err := cl.parentClient(ctx)
	if err != nil {
		return nil, err
	}
	provider, ok := parent.loginMetadata().Providers[providerID]
	if !ok || !provider.Enabled {
		return nil, fmt.Errorf("provider %s is not available", providerID)
	}
	model, ok := resolveModelForProvider(provider, identifier)
	if !ok {
		return nil, fmt.Errorf("unknown AI model %s", identifier)
	}
	resp := modelContact(provider, model)
	if !createChat {
		return resp, nil
	}
	portalKey := newAIChatPortalKey(parent.UserLogin.ID)
	portal, err := cl.Main.Bridge.GetPortalByKey(ctx, portalKey)
	if err != nil {
		return nil, err
	}
	name := defaultConversationTitle(provider, model)
	roomType := database.RoomTypeDM
	if portal.MXID == "" {
		if err = portal.CreateMatrixRoom(ctx, parent.UserLogin, &bridgev2.ChatInfo{Name: &name, Type: &roomType}); err != nil {
			return nil, err
		}
	}
	if _, err = parent.writeRoomModelState(ctx, portal, provider.ID+"/"+model.ID, ""); err != nil {
		return nil, err
	}
	resp.Chat = &bridgev2.CreateChatResponse{
		PortalKey: portalKey,
		Portal:    portal,
		PortalInfo: &bridgev2.ChatInfo{
			Name: &name,
			Type: &roomType,
		},
	}
	return resp, nil
}

func (cl *ProviderRemoteClient) parentClient(ctx context.Context) (*Client, string, error) {
	parent, providerID, err := cl.parentLogin(ctx)
	if err != nil {
		return nil, "", err
	}
	if parent.Client == nil {
		if err = cl.Main.LoadUserLogin(ctx, parent); err != nil {
			return nil, "", err
		}
	}
	client, ok := parent.Client.(*Client)
	if !ok {
		return nil, "", fmt.Errorf("parent login %s is not an AI main login", parent.ID)
	}
	return client, providerID, nil
}

func (cl *ProviderRemoteClient) parentLogin(ctx context.Context) (*bridgev2.UserLogin, string, error) {
	meta, ok := cl.UserLogin.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta.Kind != aiid.LoginKindProvider || meta.ParentLoginID == "" || meta.ProviderID == "" {
		return nil, "", fmt.Errorf("login %s is not a provider remote", cl.UserLogin.ID)
	}
	parentID := networkid.UserLoginID(meta.ParentLoginID)
	if cl.Main == nil || cl.Main.Bridge == nil {
		return &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			ID:       parentID,
			UserMXID: cl.UserLogin.UserMXID,
			Metadata: &aiid.UserLoginMetadata{
				Kind: aiid.LoginKindMain,
			},
		}}, meta.ProviderID, nil
	}
	parent, err := cl.Main.Bridge.GetExistingUserLoginByID(ctx, parentID)
	if err != nil {
		return nil, "", err
	}
	if parent == nil || parent.UserMXID != cl.UserLogin.UserMXID {
		return nil, "", fmt.Errorf("parent login %s is unavailable", parentID)
	}
	return parent, meta.ProviderID, nil
}

func (cl *ProviderRemoteClient) provider(ctx context.Context) (aiid.ProviderConfig, bool, error) {
	parent, providerID, err := cl.parentClient(ctx)
	if err != nil {
		return aiid.ProviderConfig{}, false, err
	}
	provider, ok := parent.loginMetadata().Providers[providerID]
	if !ok || !provider.Enabled {
		return aiid.ProviderConfig{}, false, nil
	}
	return provider, true, nil
}

func (cl *ProviderRemoteClient) providerAvailable(ctx context.Context) bool {
	_, ok, err := cl.provider(ctx)
	return err == nil && ok
}

func (cl *ProviderRemoteClient) sendBridgeState(state status.BridgeStateEvent) {
	if cl != nil && cl.UserLogin != nil && cl.UserLogin.BridgeState != nil {
		cl.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: state})
	}
}
