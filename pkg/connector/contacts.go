package connector

import (
	"context"
	"fmt"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
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
		portalKey := aiid.ModelPortalKey(provider.ID, model.ID, cl.UserLogin.ID)
		portal, err := cl.Main.Bridge.GetPortalByKey(ctx, portalKey)
		if err != nil {
			return nil, err
		}
		meta := portalMetadata(portal)
		meta.SelectedLoginID = string(cl.UserLogin.ID)
		meta.SelectedProviderID = provider.ID
		meta.SelectedModelID = model.ID
		if err = portal.Save(ctx); err != nil {
			return nil, err
		}
		name := modelDisplayName(provider, model)
		resp.Chat = &bridgev2.CreateChatResponse{
			PortalKey: portalKey,
			Portal:    portal,
			PortalInfo: &bridgev2.ChatInfo{
				Name: &name,
			},
		}
	}
	return resp, nil
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
		for _, model := range contactModels(provider) {
			name := strings.ToLower(modelDisplayName(provider, model))
			if query != "" && !strings.Contains(name, query) && !strings.Contains(strings.ToLower(model.ID), query) && !strings.Contains(strings.ToLower(provider.ID), query) {
				continue
			}
			contacts = append(contacts, cl.modelContact(provider, model))
		}
	}
	return contacts
}

func (cl *Client) modelContact(provider aiid.ProviderConfig, model ai.Model) *bridgev2.ResolveIdentifierResponse {
	name := modelDisplayName(provider, model)
	isBot := true
	return &bridgev2.ResolveIdentifierResponse{
		UserID: aiid.AssistantUserID(provider.ID, model.ID),
		UserInfo: &bridgev2.UserInfo{
			Name:        &name,
			IsBot:       &isBot,
			Identifiers: []string{provider.ID + "/" + model.ID, model.ID},
		},
	}
}

func (cl *Client) resolveModelIdentifier(identifier string) (aiid.ProviderConfig, ai.Model, bool) {
	meta := cl.loginMetadata()
	if meta == nil {
		return aiid.ProviderConfig{}, ai.Model{}, false
	}
	if providerID, modelID, ok := aiid.ParseAssistantUserID(networkid.UserID(identifier)); ok {
		identifier = providerID + "/" + modelID
	}
	for _, provider := range meta.Providers {
		if !provider.Enabled {
			continue
		}
		for _, model := range contactModels(provider) {
			if identifier == string(aiid.AssistantUserID(provider.ID, model.ID)) || identifier == provider.ID+"/"+model.ID || identifier == model.ID {
				return provider, model, true
			}
		}
	}
	return aiid.ProviderConfig{}, ai.Model{}, false
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
		ensureMetadataDefaults(meta, cl.Main.defaultProviderConfig())
	}
	return meta
}

func contactModels(provider aiid.ProviderConfig) []ai.Model {
	if len(provider.Models) > 0 {
		return provider.Models
	}
	if provider.DefaultModel == "" {
		return nil
	}
	return []ai.Model{{ID: provider.DefaultModel, Name: provider.DefaultModel, Provider: provider.Provider, API: provider.API, BaseURL: provider.BaseURL}}
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
