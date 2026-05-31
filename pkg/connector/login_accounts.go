package connector

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/id"
)

func (c *Connector) ensureAIChatsMetadata(ctx context.Context, login *bridgev2.UserLogin) error {
	if login == nil {
		return fmt.Errorf("missing AI login")
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		meta = &aiid.UserLoginMetadata{}
		login.Metadata = meta
	}
	provider := c.defaultProviderConfig(login.UserMXID)
	if provider.BaseURL == "" {
		return fmt.Errorf("Beeper AI is not available for %s", login.UserMXID.Homeserver())
	}
	if meta.Providers == nil {
		meta.Providers = map[string]aiid.ProviderConfig{}
	}
	if !reflect.DeepEqual(meta.Providers[aiid.DefaultProvider], provider) {
		meta.Providers[aiid.DefaultProvider] = provider
		if client, ok := login.Client.(*Client); ok {
			client.invalidateModelCaches()
		}
		return login.Save(ctx)
	}
	return nil
}

func (c *Connector) providerForLogin(login *bridgev2.UserLogin, providerID string) (aiid.ProviderConfig, bool) {
	if login == nil {
		return aiid.ProviderConfig{}, false
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		return aiid.ProviderConfig{}, false
	}
	if providerID == "" {
		providerID = aiid.DefaultProvider
	}
	provider, ok := meta.Providers[providerID]
	if !ok || provider.ID == "" {
		return aiid.ProviderConfig{}, false
	}
	return provider, true
}

func (c *Connector) providersForLogin(login *bridgev2.UserLogin) map[string]aiid.ProviderConfig {
	providers := map[string]aiid.ProviderConfig{}
	if login == nil {
		return providers
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		return providers
	}
	for id, provider := range meta.Providers {
		if id != "" && provider.ID != "" {
			providers[id] = provider
		}
	}
	return providers
}

func (c *Connector) connectUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	if login == nil {
		return nil
	}
	if login.Client == nil {
		if err := c.LoadUserLogin(ctx, login); err != nil {
			return err
		}
	}
	if login.Client != nil {
		login.Client.Connect(ctx)
	}
	return nil
}

func (c *Connector) aiChatsClient(ctx context.Context, user *bridgev2.User) (*Client, error) {
	main, err := c.EnsureAIChatsLogin(ctx, user)
	if err != nil {
		return nil, err
	}
	if main.Client == nil {
		if err = c.LoadUserLogin(ctx, main); err != nil {
			return nil, err
		}
	}
	client, ok := main.Client.(*Client)
	if !ok {
		return nil, fmt.Errorf("AI Chats login %s is not loaded as an AI client", main.ID)
	}
	return client, nil
}

func (c *Connector) EnsureAIChatsLogin(ctx context.Context, user *bridgev2.User) (*bridgev2.UserLogin, error) {
	return c.CreateAIChatsLogin(ctx, user, "", "")
}

func (c *Connector) CreateAIChatsLogin(ctx context.Context, user *bridgev2.User, requestedLoginID networkid.UserLoginID, displayName string) (*bridgev2.UserLogin, error) {
	if c == nil || c.Bridge == nil || user == nil {
		return nil, fmt.Errorf("Beeper AI login requires a bridge and user")
	}
	loginID, duplicate, err := c.aiChatsLoginIDForUser(ctx, user, requestedLoginID)
	if err != nil {
		return nil, err
	}
	displayName = strings.TrimSpace(displayName)
	if cached := c.Bridge.GetCachedUserLoginByID(loginID); cached != nil {
		if cached.UserMXID != user.MXID {
			return nil, fmt.Errorf("AI Chats login %s belongs to %s", loginID, cached.UserMXID)
		}
		if displayName != "" {
			cached.RemoteName = displayName
			cached.RemoteProfile.Name = displayName
		}
		if err := c.ensureAIChatsMetadata(ctx, cached); err != nil {
			return nil, err
		}
		if err := cached.Save(ctx); err != nil {
			return nil, err
		}
		if err := c.deleteDuplicateAIChatsLogin(ctx, duplicate); err != nil {
			return nil, err
		}
		if err := c.connectUserLogin(ctx, cached); err != nil {
			return nil, err
		}
		return cached, nil
	}
	provider := c.defaultProviderConfig(user.MXID)
	if provider.BaseURL == "" {
		return nil, fmt.Errorf("Beeper AI is not available for %s", user.MXID.Homeserver())
	}
	if displayName == "" {
		displayName = "Beeper AI"
		if loginID != c.defaultLoginID(user.MXID) {
			displayName = string(loginID)
		}
	}
	metadata := &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
		provider.ID: provider,
	}}
	if duplicate != nil {
		if duplicateMeta, ok := duplicate.Metadata.(*aiid.UserLoginMetadata); ok && duplicateMeta != nil && len(duplicateMeta.Providers) > 0 {
			metadata = duplicateMeta
		}
	}
	login, err := user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: displayName,
		RemoteProfile: status.RemoteProfile{
			Name: displayName,
		},
		Metadata: metadata,
	}, &bridgev2.NewLoginParams{})
	if err != nil {
		return nil, err
	}
	if err = c.deleteDuplicateAIChatsLogin(ctx, duplicate); err != nil {
		return nil, err
	}
	if err = c.connectUserLogin(ctx, login); err != nil {
		return nil, err
	}
	return login, nil
}

func (c *Connector) aiChatsLoginIDForUser(ctx context.Context, user *bridgev2.User, requestedLoginID networkid.UserLoginID) (networkid.UserLoginID, *bridgev2.UserLogin, error) {
	loginID := networkid.UserLoginID(strings.TrimSpace(string(requestedLoginID)))
	isDefaultLogin := loginID == ""
	if loginID == "" {
		loginID = c.defaultLoginID(user.MXID)
	} else if err := validateAIChatsLoginID(loginID); err != nil {
		return "", nil, err
	}
	if !isDefaultLogin {
		return loginID, nil, nil
	}
	perUserID := c.perUserDefaultLoginID(user.MXID)
	if loginID == perUserID {
		return loginID, nil, nil
	}
	existingPreferred, err := c.Bridge.GetExistingUserLoginByID(ctx, loginID)
	if err != nil {
		return "", nil, err
	}
	if existingPreferred != nil && existingPreferred.UserMXID != user.MXID {
		return perUserID, nil, nil
	}
	existingPerUser, err := c.Bridge.GetExistingUserLoginByID(ctx, perUserID)
	if err != nil {
		return "", nil, err
	}
	if existingPerUser != nil && existingPerUser.UserMXID == user.MXID {
		return loginID, existingPerUser, nil
	}
	return loginID, nil, nil
}

func validateAIChatsLoginID(loginID networkid.UserLoginID) error {
	if loginID == "" {
		return fmt.Errorf("login id is required")
	}
	if loginID == "all" {
		return fmt.Errorf("login id %q is reserved", loginID)
	}
	if strings.ContainsAny(string(loginID), " \t\r\n/?#") {
		return fmt.Errorf("login id may not contain whitespace or URL separators")
	}
	return nil
}

func (c *Connector) deleteDuplicateAIChatsLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	if login == nil {
		return nil
	}
	login.Log.Info().Str("replacement_login_id", string(c.defaultLoginID(login.UserMXID))).Msg("Deleting duplicate AI Chats login")
	login.Delete(ctx, status.BridgeState{}, bridgev2.DeleteOpts{
		LogoutRemote:     false,
		DontCleanupRooms: true,
	})
	return nil
}

func (c *Connector) ensureAIChatsLoginsForPersistedUsers(ctx context.Context) error {
	if c == nil || c.Bridge == nil || c.Bridge.DB == nil {
		return nil
	}
	userIDs, err := c.startupUserIDs(ctx)
	if err != nil {
		return err
	}
	for _, userID := range userIDs {
		user, err := c.Bridge.GetUserByMXID(ctx, userID)
		if err != nil {
			return err
		}
		if _, err = c.EnsureAIChatsLogin(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connector) startupUserIDs(ctx context.Context) ([]id.UserID, error) {
	seen := map[id.UserID]struct{}{}
	var userIDs []id.UserID
	add := func(userID id.UserID) {
		if _, ok := seen[userID]; ok {
			return
		}
		seen[userID] = struct{}{}
		userIDs = append(userIDs, userID)
	}
	persisted, err := c.getAllPersistedUserIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, userID := range persisted {
		add(userID)
	}
	for _, userID := range c.configuredLoginUserIDs() {
		add(userID)
	}
	return userIDs, nil
}

func (c *Connector) getAllPersistedUserIDs(ctx context.Context) ([]id.UserID, error) {
	rows, err := c.Bridge.DB.Query(ctx, `SELECT mxid FROM "user" WHERE bridge_id=$1`, c.Bridge.ID)
	return dbutil.NewRowIterWithError(rows, dbutil.ScanSingleColumn[id.UserID], err).AsList()
}

func (c *Connector) configuredLoginUserIDs() []id.UserID {
	if c == nil || c.Bridge == nil || c.Bridge.Config == nil {
		return nil
	}
	userIDs := make([]id.UserID, 0, len(c.Bridge.Config.Permissions))
	for rawUserID, permissions := range c.Bridge.Config.Permissions {
		if permissions == nil || !permissions.Login {
			continue
		}
		userID := id.UserID(rawUserID)
		if _, _, err := userID.Parse(); err != nil {
			continue
		}
		userIDs = append(userIDs, userID)
	}
	sort.Slice(userIDs, func(i, j int) bool {
		return userIDs[i] < userIDs[j]
	})
	return userIDs
}
