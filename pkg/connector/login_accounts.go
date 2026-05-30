package connector

import (
	"context"
	"fmt"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/status"
)

func (c *Connector) isAIChatsLogin(login *bridgev2.UserLogin) bool {
	return login != nil && login.ID == c.defaultLoginID(login.UserMXID)
}

func (c *Connector) ensureAIChatsMetadata(ctx context.Context, login *bridgev2.UserLogin) error {
	meta := login.Metadata.(*aiid.UserLoginMetadata)
	provider := c.defaultProviderConfig(login.UserMXID)
	if provider.BaseURL == "" {
		return fmt.Errorf("Beeper AI is not available for %s", login.UserMXID.Homeserver())
	}
	if meta.Provider == nil ||
		meta.Provider.ID != provider.ID ||
		meta.Provider.DisplayName != provider.DisplayName ||
		meta.Provider.API != provider.API ||
		meta.Provider.Provider != provider.Provider ||
		meta.Provider.BaseURL != provider.BaseURL ||
		meta.Provider.DefaultModel != provider.DefaultModel {
		meta.Provider = &provider
		return login.Save(ctx)
	}
	return nil
}

func (c *Connector) providerFromLogin(login *bridgev2.UserLogin) (aiid.ProviderConfig, bool) {
	if login == nil {
		return aiid.ProviderConfig{}, false
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta.Provider == nil || meta.Provider.ID == "" {
		return aiid.ProviderConfig{}, false
	}
	return *meta.Provider, true
}

func (c *Connector) providerLoginForID(login *bridgev2.UserLogin, providerID string) *bridgev2.UserLogin {
	if login == nil || login.User == nil || providerID == "" {
		return nil
	}
	mainID := c.defaultLoginID(login.UserMXID)
	loginID := aiid.ProviderLoginID(mainID, providerID)
	if login.ID == loginID {
		return login
	}
	for _, candidate := range login.User.GetUserLogins() {
		if candidate.ID == loginID {
			return candidate
		}
	}
	return nil
}

func (c *Connector) providerForLogin(login *bridgev2.UserLogin, providerID string) (aiid.ProviderConfig, bool) {
	if providerID == "" {
		providerID = aiid.DefaultProvider
	}
	if provider, ok := c.providerFromLogin(login); ok && provider.ID == providerID {
		return provider, true
	}
	if providerID == aiid.DefaultProvider {
		main, err := c.aiChatsLoginFromLoadedUser(login)
		if err != nil {
			return aiid.ProviderConfig{}, false
		}
		provider, ok := c.providerFromLogin(main)
		return provider, ok
	}
	providerLogin := c.providerLoginForID(login, providerID)
	provider, ok := c.providerFromLogin(providerLogin)
	return provider, ok
}

func (c *Connector) aiChatsLoginFromLoadedUser(login *bridgev2.UserLogin) (*bridgev2.UserLogin, error) {
	if login == nil {
		return nil, fmt.Errorf("missing login")
	}
	mainID := c.defaultLoginID(login.UserMXID)
	if login.ID == mainID {
		return login, nil
	}
	if login.User != nil {
		for _, candidate := range login.User.GetUserLogins() {
			if candidate.ID == mainID {
				return candidate, nil
			}
		}
	}
	if c.Bridge == nil {
		return nil, fmt.Errorf("AI Chats login %s is unavailable", mainID)
	}
	main := c.Bridge.GetCachedUserLoginByID(mainID)
	if main == nil || main.UserMXID != login.UserMXID {
		return nil, fmt.Errorf("AI Chats login %s is unavailable", mainID)
	}
	return main, nil
}

func (c *Connector) providersForLogin(login *bridgev2.UserLogin) map[string]aiid.ProviderConfig {
	providers := map[string]aiid.ProviderConfig{}
	if provider, ok := c.providerForLogin(login, aiid.DefaultProvider); ok {
		providers[provider.ID] = provider
	}
	if login != nil && login.User != nil {
		mainID := c.defaultLoginID(login.UserMXID)
		for _, userLogin := range login.User.GetUserLogins() {
			if userLogin.ID == mainID {
				continue
			}
			if provider, ok := c.providerFromLogin(userLogin); ok {
				providers[provider.ID] = provider
			}
		}
	} else if provider, ok := c.providerFromLogin(login); ok && provider.ID != aiid.DefaultProvider {
		providers[provider.ID] = provider
	}
	return providers
}

func (c *Connector) getAIChatsLogin(ctx context.Context, user *bridgev2.User) (*bridgev2.UserLogin, error) {
	if c == nil || c.Bridge == nil || user == nil {
		return nil, nil
	}
	loginID := c.defaultLoginID(user.MXID)
	if cached := c.Bridge.GetCachedUserLoginByID(loginID); cached != nil && cached.UserMXID == user.MXID {
		return cached, nil
	}
	login, err := c.Bridge.GetExistingUserLoginByID(ctx, loginID)
	if err != nil {
		return nil, err
	}
	if login == nil || login.UserMXID != user.MXID {
		return nil, nil
	}
	return login, nil
}

func (c *Connector) requireAIChatsLogin(ctx context.Context, user *bridgev2.User) (*bridgev2.UserLogin, error) {
	login, err := c.getAIChatsLogin(ctx, user)
	if err != nil {
		return nil, err
	}
	if login == nil {
		return nil, fmt.Errorf("add Beeper AI before adding custom providers")
	}
	return login, nil
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

func (c *Connector) providerLoginClientForLogin(ctx context.Context, login *bridgev2.UserLogin, providerID string) (*ProviderLoginClient, bool) {
	providerLogin := c.providerLoginForID(login, providerID)
	if providerLogin == nil {
		return nil, false
	}
	if providerLogin.Client == nil {
		if err := c.LoadUserLogin(ctx, providerLogin); err != nil {
			return nil, false
		}
	}
	client, ok := providerLogin.Client.(*ProviderLoginClient)
	return client, ok
}

func (c *Connector) aiChatsClient(ctx context.Context, user *bridgev2.User) (*Client, error) {
	main, err := c.requireAIChatsLogin(ctx, user)
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
	loginID := c.defaultLoginID(user.MXID)
	if cached := c.Bridge.GetCachedUserLoginByID(loginID); cached != nil {
		if err := c.ensureAIChatsMetadata(ctx, cached); err != nil {
			return nil, err
		}
		return cached, nil
	}
	provider := c.defaultProviderConfig(user.MXID)
	if provider.BaseURL == "" {
		return nil, fmt.Errorf("Beeper AI is not available for %s", user.MXID.Homeserver())
	}
	return user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: aiid.DefaultLoginName,
		RemoteProfile: status.RemoteProfile{
			Name: "Beeper AI",
		},
		Metadata: &aiid.UserLoginMetadata{Provider: &provider},
	}, &bridgev2.NewLoginParams{})
}
