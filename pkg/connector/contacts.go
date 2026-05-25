package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func (cl *Client) GetContactList(ctx context.Context) ([]*bridgev2.ResolveIdentifierResponse, error) {
	return cl.modelContacts(ctx, ""), nil
}

func (cl *Client) SearchUsers(ctx context.Context, query string) ([]*bridgev2.ResolveIdentifierResponse, error) {
	return cl.modelContacts(ctx, strings.ToLower(strings.TrimSpace(query))), nil
}

func (cl *Client) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	provider, model, ok := cl.resolveModelIdentifier(identifier)
	if !ok {
		return nil, fmt.Errorf("unknown AI model %s", identifier)
	}
	resp := cl.modelContact(provider, model)
	if createChat {
		portalKey := newAIChatPortalKey(cl.UserLogin.ID)
		portal, err := cl.Main.Bridge.GetPortalByKey(ctx, portalKey)
		if err != nil {
			return nil, err
		}
		name := defaultConversationTitle(provider, model)
		roomType := database.RoomTypeDM
		if portal.MXID == "" {
			if err = portal.CreateMatrixRoom(ctx, cl.UserLogin, &bridgev2.ChatInfo{Name: &name, Type: &roomType}); err != nil {
				return nil, err
			}
		}
		if _, err = cl.writeRoomModelState(ctx, portal, provider.ID+"/"+model.ID, ""); err != nil {
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
	}
	return resp, nil
}

func newAIChatPortalKey(loginID networkid.UserLoginID) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       networkid.PortalID("chat:" + session.CreateSessionID()),
		Receiver: loginID,
	}
}

func (cl *Client) modelContacts(ctx context.Context, query string) []*bridgev2.ResolveIdentifierResponse {
	meta := cl.loginMetadata()
	if meta == nil {
		return nil
	}
	contacts := []*bridgev2.ResolveIdentifierResponse{}
	for _, provider := range meta.Providers {
		if !provider.Enabled {
			continue
		}
		contacts = append(contacts, providerModelContacts(provider, query)...)
	}
	return contacts
}

func (cl *Client) modelContact(provider aiid.ProviderConfig, model ai.Model) *bridgev2.ResolveIdentifierResponse {
	return modelContact(provider, model)
}

func providerModelContacts(provider aiid.ProviderConfig, query string) []*bridgev2.ResolveIdentifierResponse {
	contacts := []*bridgev2.ResolveIdentifierResponse{}
	for _, model := range contactModels(provider) {
		name := strings.ToLower(modelDisplayName(provider, model))
		if query != "" && !strings.Contains(name, query) && !strings.Contains(strings.ToLower(model.ID), query) && !strings.Contains(strings.ToLower(provider.ID), query) {
			continue
		}
		contacts = append(contacts, modelContact(provider, model))
	}
	return contacts
}

func modelContact(provider aiid.ProviderConfig, model ai.Model) *bridgev2.ResolveIdentifierResponse {
	name := modelDisplayName(provider, model)
	isBot := true
	return &bridgev2.ResolveIdentifierResponse{
		UserID: aiid.ModelContactID(provider.ID, model.ID),
		UserInfo: &bridgev2.UserInfo{
			Name:        &name,
			IsBot:       &isBot,
			Identifiers: []string{provider.ID + "/" + model.ID, model.ID},
		},
	}
}

func resolveModelForProvider(provider aiid.ProviderConfig, identifier string) (ai.Model, bool) {
	if providerID, modelID, ok := aiid.ParseModelContactID(aiidNetworkID(identifier)); ok {
		identifier = providerID + "/" + modelID
	}
	for _, model := range contactModels(provider) {
		if identifier == string(aiid.ModelContactID(provider.ID, model.ID)) || identifier == provider.ID+"/"+model.ID || identifier == model.ID {
			return model, true
		}
	}
	return ai.Model{}, false
}

func (cl *Client) resolveModelIdentifier(identifier string) (aiid.ProviderConfig, ai.Model, bool) {
	meta := cl.loginMetadata()
	if meta == nil {
		return aiid.ProviderConfig{}, ai.Model{}, false
	}
	if providerID, modelID, ok := aiid.ParseModelContactID(aiidNetworkID(identifier)); ok {
		identifier = providerID + "/" + modelID
	}
	for _, provider := range meta.Providers {
		if !provider.Enabled {
			continue
		}
		if model, ok := resolveModelForProvider(provider, identifier); ok {
			return provider, model, true
		}
	}
	return aiid.ProviderConfig{}, ai.Model{}, false
}

func aiidNetworkID(identifier string) networkid.UserID {
	return networkid.UserID(identifier)
}

func (cl *Client) loginMetadata() *aiid.UserLoginMetadata {
	if cl == nil || cl.UserLogin == nil {
		return nil
	}
	meta, ok := cl.UserLogin.Metadata.(*aiid.UserLoginMetadata)
	if !ok {
		return nil
	}
	if cl.Main != nil {
		ensureMetadataDefaults(meta, cl.Main.defaultProviderConfig(), cl.Main.configuredProviders())
	}
	return meta
}

func contactModels(provider aiid.ProviderConfig) []ai.Model {
	if len(provider.Models) > 0 {
		return provider.Models
	}
	if len(provider.AllowedModels) > 0 {
		models := make([]ai.Model, 0, len(provider.AllowedModels))
		for _, modelID := range provider.AllowedModels {
			if modelID == "" {
				continue
			}
			models = append(models, normalizeProviderModel(modelForProviderCatalog(provider, modelID), provider))
		}
		return models
	}
	if models := ai.GetModels(provider.Provider); len(models) > 0 {
		out := make([]ai.Model, 0, len(models))
		for _, model := range models {
			out = append(out, normalizeProviderModel(model, provider))
		}
		return out
	}
	if provider.DefaultModel == "" {
		return nil
	}
	return []ai.Model{normalizeProviderModel(modelForProviderCatalog(provider, provider.DefaultModel), provider)}
}

func modelDisplayName(provider aiid.ProviderConfig, model ai.Model) string {
	if model.Name != "" && model.Name != model.ID {
		return model.Name
	}
	if provider.DisplayName != "" {
		return provider.DisplayName + " " + model.ID
	}
	return model.ID
}

func defaultConversationTitle(provider aiid.ProviderConfig, model ai.Model) string {
	return "New AI Chat with " + modelDisplayName(provider, model)
}
