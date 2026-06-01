package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

type modelContactsCache struct {
	contacts []*bridgev2.ResolveIdentifierResponse
	valid    bool
}

type providerCatalogCache struct {
	providerKey string
	models      []ai.Model
}

func (cl *Client) GetContactList(ctx context.Context) ([]*bridgev2.ResolveIdentifierResponse, error) {
	return cl.modelContacts(ctx, ""), nil
}

func (cl *Client) SearchUsers(ctx context.Context, query string) ([]*bridgev2.ResolveIdentifierResponse, error) {
	return cl.modelContacts(ctx, strings.TrimSpace(query)), nil
}

func (cl *Client) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	provider, model, ok := cl.resolveModelIdentifier(ctx, identifier)
	if !ok {
		return nil, fmt.Errorf("unknown AI model %s", identifier)
	}
	resp := cl.modelContact(ctx, provider, model)
	if createChat {
		chat, err := cl.createModelChat(ctx, provider, model)
		if err != nil {
			return nil, err
		}
		resp.Chat = chat
	}
	return resp, nil
}

func (cl *Client) createModelChat(ctx context.Context, provider aiid.ProviderConfig, model ai.Model) (*bridgev2.CreateChatResponse, error) {
	roomConfig := RoomConfig{ProviderID: provider.ID, ModelID: model.ID}
	resolvedProvider, resolvedModel, canonicalModel, err := cl.resolveCanonicalRoomModel(ctx, roomConfig)
	if err != nil {
		return nil, err
	}
	provider = resolvedProvider
	model = resolvedModel
	portalKey := newAIChatPortalKey(cl.UserLogin.ID)
	portal, err := cl.Main.Bridge.GetPortalByKey(ctx, portalKey)
	if err != nil {
		return nil, err
	}
	name := defaultConversationTitle(provider, model)
	topic := modelRoomDescription(provider, model)
	roomType := database.RoomTypeDM
	info := &bridgev2.ChatInfo{Name: &name, Topic: &topic, Avatar: roomModelAvatar(provider, model), Type: &roomType, Members: aiChatMembers(), ExcludeChangesFromTimeline: true}
	meta := portalMetadata(portal)
	meta.AutoTitlePending = true
	created := portal.MXID == ""
	if created {
		if err = portal.CreateMatrixRoom(ctx, cl.UserLogin, info); err != nil {
			return nil, err
		}
	} else if err = portal.Save(ctx); err != nil {
		return nil, err
	}
	reasoning := cl.reasoningLevelForModel(model, roomConfig)
	reasoningMode := cl.reasoningModeForModel(model, roomConfig)
	if _, err = cl.applyRoomModelState(ctx, portal, provider, model, canonicalModel, reasoning, reasoningMode, applyRoomModelStateOptions{ForceAvatar: created}); err != nil {
		return nil, err
	}
	cl.refreshRoomCapabilities(ctx, portal)
	if err = cl.sendCommandNotice(ctx, portal, modelWelcomeNoticeText(provider, model)); err != nil {
		return nil, err
	}
	return &bridgev2.CreateChatResponse{
		PortalKey:      portalKey,
		Portal:         portal,
		PortalInfo:     info,
		DMRedirectedTo: aiid.AssistantUserID(),
	}, nil
}

func aiChatMembers() *bridgev2.ChatMemberList {
	return &bridgev2.ChatMemberList{
		IsFull:                     true,
		OtherUserID:                aiid.AssistantUserID(),
		ExcludeChangesFromTimeline: true,
		MemberMap: bridgev2.ChatMemberMap{
			"": {
				EventSender:      bridgev2.EventSender{IsFromMe: true},
				Membership:       event.MembershipJoin,
				MemberEventExtra: syntheticMemberEventExtra(),
			},
			aiid.AssistantUserID(): {
				EventSender:      bridgev2.EventSender{Sender: aiid.AssistantUserID()},
				Membership:       event.MembershipJoin,
				UserInfo:         aiAssistantUserInfo(),
				MemberEventExtra: syntheticMemberEventExtra(),
			},
		},
		PowerLevels: &bridgev2.PowerLevelOverrides{
			Events: map[event.Type]int{
				event.StateRoomName:                0,
				event.StateTopic:                   0,
				event.StateBeeperDisappearingTimer: 0,
			},
		},
	}
}

func syntheticMemberEventExtra() map[string]any {
	return map[string]any{
		"com.beeper.exclude_from_timeline": true,
	}
}

func aiAssistantUserInfo() *bridgev2.UserInfo {
	isBot := false
	name := "AI"
	return &bridgev2.UserInfo{
		Name:   &name,
		IsBot:  &isBot,
		Avatar: defaultAIAssistantAvatar(),
	}
}

func newAIChatPortalKey(loginID networkid.UserLoginID) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       networkid.PortalID("chat:" + session.CreateSessionID()),
		Receiver: loginID,
	}
}

func (cl *Client) modelContacts(ctx context.Context, query string) []*bridgev2.ResolveIdentifierResponse {
	contacts := cl.cachedListedModelContacts(ctx)
	query = strings.TrimSpace(query)
	if query == "" {
		return contacts
	}
	lowerQuery := strings.ToLower(query)
	filtered := make([]*bridgev2.ResolveIdentifierResponse, 0, len(contacts))
	seen := map[networkid.UserID]bool{}
	for _, contact := range contacts {
		if !modelContactMatchesQuery(contact, lowerQuery) {
			continue
		}
		seen[contact.UserID] = true
		filtered = append(filtered, contact)
	}
	return filtered
}

func (cl *Client) cachedListedModelContacts(ctx context.Context) []*bridgev2.ResolveIdentifierResponse {
	cl.contactCacheMu.Lock()
	if cl.contactCache.valid {
		contacts := cloneModelContacts(cl.contactCache.contacts)
		cl.contactCacheMu.Unlock()
		cl.refreshModelContactCacheAsync(ctx)
		return contacts
	}
	cl.contactCacheMu.Unlock()
	contacts, cacheable := cl.buildListedModelContacts(ctx, true)
	if cacheable {
		cl.setModelContactCache(contacts)
	}
	return contacts
}

func (cl *Client) buildListedModelContacts(ctx context.Context, refresh bool) ([]*bridgev2.ResolveIdentifierResponse, bool) {
	providers := cl.providers()
	if len(providers) == 0 {
		return nil, false
	}
	cacheable := true
	contacts := []*bridgev2.ResolveIdentifierResponse{}
	for _, provider := range providers {
		var ok bool
		provider, ok = cl.providerForModelContacts(ctx, provider, refresh)
		if !ok {
			cacheable = false
			continue
		}
		contacts = append(contacts, listedProviderModelContacts(ctx, cl.bridge(), provider)...)
	}
	return contacts, cacheable
}

func (cl *Client) setModelContactCache(contacts []*bridgev2.ResolveIdentifierResponse) {
	cl.contactCacheMu.Lock()
	cl.contactCache = modelContactsCache{
		contacts: cloneModelContacts(contacts),
		valid:    true,
	}
	cl.contactCacheMu.Unlock()
}

func (cl *Client) invalidateModelContactCache() {
	cl.contactCacheMu.Lock()
	cl.contactCache = modelContactsCache{}
	cl.contactCacheMu.Unlock()
}

func (cl *Client) invalidateModelCaches() {
	cl.invalidateModelContactCache()
	cl.catalogCacheMu.Lock()
	cl.catalogCache = providerCatalogCache{}
	cl.catalogCacheMu.Unlock()
}

func (cl *Client) refreshModelContactCacheAsync(ctx context.Context) {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		if !cl.contactRefreshMu.TryLock() {
			return
		}
		defer cl.contactRefreshMu.Unlock()
		contacts, cacheable := cl.buildListedModelContacts(ctx, true)
		if !cacheable {
			return
		}
		cl.setModelContactCache(contacts)
		zerolog.Ctx(ctx).Debug().
			Str("action", "ai_model_contacts_cache").
			Str("login_id", string(cl.UserLogin.ID)).
			Int("contact_count", len(contacts)).
			Msg("Warmed AI model contacts cache")
	}()
}

func modelContactMatchesQuery(contact *bridgev2.ResolveIdentifierResponse, lowerQuery string) bool {
	if contact == nil {
		return false
	}
	if strings.Contains(strings.ToLower(string(contact.UserID)), lowerQuery) {
		return true
	}
	if contact.UserInfo != nil {
		if contact.UserInfo.Name != nil && strings.Contains(strings.ToLower(*contact.UserInfo.Name), lowerQuery) {
			return true
		}
		for _, identifier := range contact.UserInfo.Identifiers {
			if strings.Contains(strings.ToLower(identifier), lowerQuery) {
				return true
			}
		}
	}
	if contact.Ghost != nil {
		if strings.Contains(strings.ToLower(contact.Ghost.Name), lowerQuery) {
			return true
		}
		for _, identifier := range contact.Ghost.Identifiers {
			if strings.Contains(strings.ToLower(identifier), lowerQuery) {
				return true
			}
		}
	}
	return false
}

func (cl *Client) modelContact(ctx context.Context, provider aiid.ProviderConfig, model ai.Model) *bridgev2.ResolveIdentifierResponse {
	return modelContactWithGhost(ctx, cl.bridge(), provider, model)
}

func (cl *Client) providerWithCatalogModels(ctx context.Context, provider aiid.ProviderConfig) aiid.ProviderConfig {
	if refreshed, err := cl.providerWithCatalogModelsStrict(ctx, provider); err == nil {
		return refreshed
	}
	return provider
}

func (cl *Client) providerForModelContacts(ctx context.Context, provider aiid.ProviderConfig, refresh bool) (aiid.ProviderConfig, bool) {
	refreshed, err := cl.providerWithCatalogModelsStrictWithRefresh(ctx, provider, refresh)
	if err != nil {
		zerolog.Ctx(ctx).Warn().
			Err(err).
			Str("action", "ai_model_catalog").
			Str("provider_id", provider.ID).
			Msg("Skipping provider contacts after model catalog fetch failed")
		return refreshed, false
	}
	if len(refreshed.Models) == 0 {
		zerolog.Ctx(ctx).Warn().
			Str("action", "ai_model_catalog").
			Str("provider_id", provider.ID).
			Msg("Skipping provider contacts because model catalog is empty")
		return refreshed, false
	}
	return refreshed, true
}

func (cl *Client) providerWithCatalogModelsStrict(ctx context.Context, provider aiid.ProviderConfig) (aiid.ProviderConfig, error) {
	return cl.providerWithCatalogModelsStrictWithRefresh(ctx, provider, false)
}

func (cl *Client) providerWithCatalogModelsStrictWithRefresh(ctx context.Context, provider aiid.ProviderConfig, refresh bool) (aiid.ProviderConfig, error) {
	models, err := cl.cachedAIServicesCatalogModels(ctx, provider, refresh)
	if err != nil {
		return provider, err
	}
	if len(models) > 0 {
		provider.Models = mergeProviderCatalogModels(provider, models)
		zerolog.Ctx(ctx).Debug().
			Str("action", "ai_model_catalog").
			Str("provider_id", provider.ID).
			Int("model_count", len(models)).
			Msg("Loaded AI Services model catalog")
	}
	return provider, nil
}

func mergeProviderCatalogModels(provider aiid.ProviderConfig, catalog []ai.Model) []ai.Model {
	if provider.ID == aiid.DefaultProvider || len(provider.Models) == 0 {
		return cloneModels(catalog)
	}
	byID := make(map[string]ai.Model, len(catalog))
	for _, model := range catalog {
		byID[model.ID] = model
	}
	models := make([]ai.Model, 0, len(provider.Models))
	for _, configured := range provider.Models {
		model := normalizeProviderModel(configured, provider)
		if catalogModel, ok := byID[model.ID]; ok {
			model = mergeProviderCatalogModel(model, catalogModel)
		}
		models = append(models, model)
	}
	return models
}

func mergeProviderCatalogModel(configured ai.Model, catalog ai.Model) ai.Model {
	model := catalog
	if configured.Name != "" && configured.Name != configured.ID {
		model.Name = configured.Name
	}
	if len(configured.Input) > 0 {
		model.Input = slices.Clone(configured.Input)
	}
	if len(configured.Output) > 0 {
		model.Output = slices.Clone(configured.Output)
	}
	if len(configured.BuiltInTools) > 0 {
		model.BuiltInTools = slices.Clone(configured.BuiltInTools)
	}
	if configured.ContextWindow != 0 {
		model.ContextWindow = configured.ContextWindow
	}
	if configured.MaxTokens != 0 {
		model.MaxTokens = configured.MaxTokens
	}
	if configured.Headers != nil {
		model.Headers = maps.Clone(configured.Headers)
	}
	if configured.Compat != nil {
		compat := maps.Clone(model.Compat)
		for key, value := range configured.Compat {
			compat[key] = value
		}
		model.Compat = compat
	}
	return model
}

func (cl *Client) cachedAIServicesCatalogModels(ctx context.Context, provider aiid.ProviderConfig, refresh bool) ([]ai.Model, error) {
	providerKey := providerCatalogCacheKey(provider)
	cl.catalogCacheMu.Lock()
	if !refresh && providerKey == cl.catalogCache.providerKey {
		models := cloneModels(cl.catalogCache.models)
		cl.catalogCacheMu.Unlock()
		return models, nil
	}
	cl.catalogCacheMu.Unlock()
	models, err := cl.aiServicesCatalogModels(ctx, provider)
	if err != nil {
		return nil, err
	}
	if len(models) > 0 {
		cl.catalogCacheMu.Lock()
		cl.catalogCache = providerCatalogCache{
			providerKey: providerKey,
			models:      cloneModels(models),
		}
		cl.catalogCacheMu.Unlock()
	}
	return models, nil
}

func providerCatalogCacheKey(provider aiid.ProviderConfig) string {
	return provider.ID + "\x00" + string(provider.API) + "\x00" + string(provider.Provider) + "\x00" + provider.BaseURL
}

func (cl *Client) bridge() *bridgev2.Bridge {
	if cl == nil || cl.Main == nil {
		return nil
	}
	return cl.Main.Bridge
}

func listedProviderModelContacts(ctx context.Context, br *bridgev2.Bridge, provider aiid.ProviderConfig) []*bridgev2.ResolveIdentifierResponse {
	contacts := []*bridgev2.ResolveIdentifierResponse{}
	for _, model := range contactModels(provider) {
		contact := modelContactWithGhost(ctx, br, provider, model)
		contacts = append(contacts, contact)
	}
	return contacts
}

func (cl *Client) aiServicesCatalogModels(ctx context.Context, provider aiid.ProviderConfig) ([]ai.Model, error) {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil || cl.Main.AppServiceToken == "" {
		return nil, nil
	}
	catalogProvider := provider
	if provider.ID != aiid.DefaultProvider {
		catalogProvider = cl.Main.defaultProviderConfig(cl.UserLogin.UserMXID)
		if catalogProvider.BaseURL == "" {
			return nil, fmt.Errorf("AI Services is not available for %s", cl.UserLogin.UserMXID.Homeserver())
		}
	}
	modelsURL, err := aiServicesModelsURL(catalogProvider.BaseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	token, err := cl.defaultProviderBearerToken()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := aiutils.WithAIServicesLogging(&http.Client{Timeout: 20 * time.Second})
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AI Services models returned HTTP %d", resp.StatusCode)
	}
	var body aiServicesModelListResponse
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	models := make([]ai.Model, 0, len(body.Data))
	for _, item := range body.Data {
		if provider.ID != aiid.DefaultProvider && !item.matchesProvider(provider.Provider) {
			continue
		}
		modelID := strings.TrimSpace(item.ID)
		if modelID == "" {
			continue
		}
		model := ai.Model{
			ID:                   modelID,
			Name:                 item.Name,
			API:                  provider.API,
			Provider:             provider.Provider,
			BaseURL:              provider.BaseURL,
			Reasoning:            item.reasoning(),
			ThinkingLevelMap:     item.thinkingLevelMap(),
			DefaultThinkingLevel: item.defaultThinkingLevel(),
			ReasoningMode:        item.reasoningMode(),
			Input:                item.inputModalities(),
			Output:               item.outputModalities(),
			ContextWindow:        item.contextWindow(),
			MaxTokens:            item.maxTokens(),
			BuiltInTools:         item.builtInTools(),
			Compat:               item.compat(),
		}
		model = item.applyRuntime(model, provider, provider.ID == aiid.DefaultProvider)
		models = append(models, normalizeProviderModel(model, provider))
	}
	return models, nil
}

func aiServicesModelsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(normalizeResponsesBaseURL(baseURL), "/"))
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/models"
	parsed.RawQuery = url.Values{"feature": {"bridge:ai"}}.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

type aiServicesModelListResponse struct {
	Type string                 `json:"type"`
	Data []aiServicesModelEntry `json:"data"`
}

type aiServicesModelEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Metadata      *struct {
		Family          string `json:"family"`
		ProviderLogoURL string `json:"provider_logo_url"`
	} `json:"metadata"`
	Architecture *struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	TopProvider *struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
	Runtime *struct {
		API      string                 `json:"api"`
		Provider string                 `json:"provider"`
		BaseURL  string                 `json:"base_url"`
		Model    string                 `json:"model"`
		Endpoint string                 `json:"endpoint"`
		Compat   *aiServicesModelCompat `json:"compat"`
	} `json:"runtime"`
	Capabilities *struct {
		Input struct {
			Modalities []string `json:"modalities"`
		} `json:"input"`
		Output struct {
			Modalities []string `json:"modalities"`
		} `json:"output"`
		Reasoning *struct {
			Supported    bool               `json:"supported"`
			Levels       []string           `json:"levels"`
			LevelMap     map[string]*string `json:"level_map"`
			DefaultLevel string             `json:"default_level"`
			Mode         string             `json:"mode"`
		} `json:"reasoning"`
		Tools *struct {
			Supported bool     `json:"supported"`
			BuiltIn   []string `json:"built_in"`
		} `json:"tools"`
		Limits *struct {
			ContextTokens int `json:"context_tokens"`
			OutputTokens  int `json:"output_tokens"`
		} `json:"limits"`
	} `json:"capabilities"`
}

type aiServicesModelCompat struct {
	SupportsStore                               *bool  `json:"supports_store,omitempty"`
	SupportsDeveloperRole                       *bool  `json:"supports_developer_role,omitempty"`
	SupportsReasoningEffort                     *bool  `json:"supports_reasoning_effort,omitempty"`
	SupportsUsageInStreaming                    *bool  `json:"supports_usage_in_streaming,omitempty"`
	MaxTokensField                              string `json:"max_tokens_field,omitempty"`
	RequiresToolResultName                      *bool  `json:"requires_tool_result_name,omitempty"`
	RequiresAssistantAfterToolResult            *bool  `json:"requires_assistant_after_tool_result,omitempty"`
	RequiresThinkingAsText                      *bool  `json:"requires_thinking_as_text,omitempty"`
	RequiresReasoningContentOnAssistantMessages *bool  `json:"requires_reasoning_content_on_assistant_messages,omitempty"`
	ThinkingFormat                              string `json:"thinking_format,omitempty"`
	ZaiToolStream                               *bool  `json:"zai_tool_stream,omitempty"`
	SupportsStrictMode                          *bool  `json:"supports_strict_mode,omitempty"`
	CacheControlFormat                          string `json:"cache_control_format,omitempty"`
	SendSessionAffinityHeaders                  *bool  `json:"send_session_affinity_headers,omitempty"`
	SupportsLongCacheRetention                  *bool  `json:"supports_long_cache_retention,omitempty"`
	SendSessionIDHeader                         *bool  `json:"send_session_id_header,omitempty"`
	SupportsEagerToolInputStreaming             *bool  `json:"supports_eager_tool_input_streaming,omitempty"`
	SupportsCacheControlOnTools                 *bool  `json:"supports_cache_control_on_tools,omitempty"`
	SupportsTemperature                         *bool  `json:"supports_temperature,omitempty"`
	ForceAdaptiveThinking                       *bool  `json:"force_adaptive_thinking,omitempty"`
	AllowEmptySignature                         *bool  `json:"allow_empty_signature,omitempty"`
}

func (entry aiServicesModelEntry) matchesProvider(provider ai.Provider) bool {
	if entry.Runtime == nil || entry.Runtime.Provider == "" {
		return provider == ""
	}
	return ai.Provider(entry.Runtime.Provider) == provider
}

func (entry aiServicesModelEntry) applyRuntime(model ai.Model, provider aiid.ProviderConfig, useRuntimeBaseURL bool) ai.Model {
	if entry.Runtime == nil {
		return model
	}
	if model.Compat == nil {
		model.Compat = map[string]any{}
	}
	if entry.Runtime.Model != "" {
		model.Compat["runtime_model"] = entry.Runtime.Model
	}
	if entry.Runtime.API != "" {
		model.API = ai.Api(entry.Runtime.API)
	}
	if entry.Runtime.Provider != "" {
		model.Provider = ai.Provider(entry.Runtime.Provider)
		model.Compat["runtime_provider"] = entry.Runtime.Provider
	}
	if useRuntimeBaseURL && entry.Runtime.BaseURL != "" {
		model.BaseURL = aiServicesRuntimeBaseURL(provider.BaseURL, entry.Runtime.BaseURL)
	}
	entry.Runtime.Compat.applyTo(model.Compat)
	return model
}

func (compat *aiServicesModelCompat) applyTo(dst map[string]any) {
	if compat == nil {
		return
	}
	setBoolCompat(dst, "supportsStore", compat.SupportsStore)
	setBoolCompat(dst, "supportsDeveloperRole", compat.SupportsDeveloperRole)
	setBoolCompat(dst, "supportsReasoningEffort", compat.SupportsReasoningEffort)
	setBoolCompat(dst, "supportsUsageInStreaming", compat.SupportsUsageInStreaming)
	setStringCompat(dst, "maxTokensField", compat.MaxTokensField)
	setBoolCompat(dst, "requiresToolResultName", compat.RequiresToolResultName)
	setBoolCompat(dst, "requiresAssistantAfterToolResult", compat.RequiresAssistantAfterToolResult)
	setBoolCompat(dst, "requiresThinkingAsText", compat.RequiresThinkingAsText)
	setBoolCompat(dst, "requiresReasoningContentOnAssistantMessages", compat.RequiresReasoningContentOnAssistantMessages)
	setStringCompat(dst, "thinkingFormat", compat.ThinkingFormat)
	setBoolCompat(dst, "zaiToolStream", compat.ZaiToolStream)
	setBoolCompat(dst, "supportsStrictMode", compat.SupportsStrictMode)
	setStringCompat(dst, "cacheControlFormat", compat.CacheControlFormat)
	setBoolCompat(dst, "sendSessionAffinityHeaders", compat.SendSessionAffinityHeaders)
	setBoolCompat(dst, "supportsLongCacheRetention", compat.SupportsLongCacheRetention)
	setBoolCompat(dst, "sendSessionIdHeader", compat.SendSessionIDHeader)
	setBoolCompat(dst, "supportsEagerToolInputStreaming", compat.SupportsEagerToolInputStreaming)
	setBoolCompat(dst, "supportsCacheControlOnTools", compat.SupportsCacheControlOnTools)
	setBoolCompat(dst, "supportsTemperature", compat.SupportsTemperature)
	setBoolCompat(dst, "forceAdaptiveThinking", compat.ForceAdaptiveThinking)
	setBoolCompat(dst, "allowEmptySignature", compat.AllowEmptySignature)
}

func setBoolCompat(dst map[string]any, key string, value *bool) {
	if value != nil {
		dst[key] = *value
	}
}

func setStringCompat(dst map[string]any, key string, value string) {
	if value != "" {
		dst[key] = value
	}
}

func (entry aiServicesModelEntry) compat() map[string]any {
	compat := map[string]any{}
	if entry.Metadata != nil {
		if entry.Metadata.ProviderLogoURL != "" {
			compat["provider_logo_url"] = entry.Metadata.ProviderLogoURL
		}
		if entry.Metadata.Family != "" {
			compat["family"] = entry.Metadata.Family
		}
	}
	if entry.Capabilities != nil && entry.Capabilities.Tools != nil {
		compat["tools_supported"] = entry.Capabilities.Tools.Supported
	}
	if len(compat) == 0 {
		return nil
	}
	return compat
}

func aiServicesRuntimeBaseURL(baseURL string, runtimeBaseURL string) string {
	parsed, err := url.Parse(strings.TrimRight(normalizeResponsesBaseURL(baseURL), "/"))
	if err != nil {
		return baseURL
	}
	parsed.Path = joinURLPath(parsed.Path, runtimeBaseURL)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (entry aiServicesModelEntry) inputModalities() []string {
	if entry.Capabilities != nil && len(entry.Capabilities.Input.Modalities) > 0 {
		return slices.Clone(entry.Capabilities.Input.Modalities)
	}
	if entry.Architecture != nil && len(entry.Architecture.InputModalities) > 0 {
		return slices.Clone(entry.Architecture.InputModalities)
	}
	return nil
}

func (entry aiServicesModelEntry) outputModalities() []string {
	if entry.Capabilities != nil && len(entry.Capabilities.Output.Modalities) > 0 {
		return slices.Clone(entry.Capabilities.Output.Modalities)
	}
	return []string{"text"}
}

func (entry aiServicesModelEntry) contextWindow() int {
	if entry.Capabilities != nil && entry.Capabilities.Limits != nil && entry.Capabilities.Limits.ContextTokens > 0 {
		return entry.Capabilities.Limits.ContextTokens
	}
	return entry.ContextLength
}

func (entry aiServicesModelEntry) maxTokens() int {
	if entry.Capabilities != nil && entry.Capabilities.Limits != nil && entry.Capabilities.Limits.OutputTokens > 0 {
		return entry.Capabilities.Limits.OutputTokens
	}
	if entry.TopProvider != nil {
		return entry.TopProvider.MaxCompletionTokens
	}
	return 0
}

func (entry aiServicesModelEntry) reasoning() bool {
	return entry.Capabilities != nil && entry.Capabilities.Reasoning != nil && entry.Capabilities.Reasoning.Supported
}

func (entry aiServicesModelEntry) reasoningMode() ai.ModelReasoningMode {
	if entry.Capabilities == nil || entry.Capabilities.Reasoning == nil {
		return ""
	}
	return ai.ModelReasoningMode(strings.ToLower(strings.TrimSpace(entry.Capabilities.Reasoning.Mode)))
}

func (entry aiServicesModelEntry) builtInTools() []string {
	if entry.Capabilities == nil || entry.Capabilities.Tools == nil || !entry.Capabilities.Tools.Supported {
		return nil
	}
	return slices.Clone(entry.Capabilities.Tools.BuiltIn)
}

func (entry aiServicesModelEntry) thinkingLevelMap() map[ai.ModelThinkingLevel]*string {
	if entry.Capabilities == nil || entry.Capabilities.Reasoning == nil || len(entry.Capabilities.Reasoning.Levels) == 0 {
		return nil
	}
	if len(entry.Capabilities.Reasoning.LevelMap) > 0 {
		out := map[ai.ModelThinkingLevel]*string{}
		for rawLevel, rawMapped := range entry.Capabilities.Reasoning.LevelMap {
			level := ai.ModelThinkingLevel(strings.ToLower(strings.TrimSpace(rawLevel)))
			if !modelThinkingLevelKnown(level) {
				continue
			}
			if rawMapped == nil {
				out[level] = nil
				continue
			}
			mapped := *rawMapped
			out[level] = &mapped
		}
		if len(out) > 0 {
			return out
		}
	}
	supportedLevels := map[ai.ModelThinkingLevel]bool{}
	for _, rawLevel := range entry.Capabilities.Reasoning.Levels {
		level := ai.ModelThinkingLevel(strings.ToLower(strings.TrimSpace(rawLevel)))
		if modelThinkingLevelKnown(level) {
			supportedLevels[level] = true
		}
	}
	if len(supportedLevels) == 0 {
		return nil
	}
	out := map[ai.ModelThinkingLevel]*string{}
	for _, level := range []ai.ModelThinkingLevel{
		ai.ModelThinkingLevelOff,
		ai.ModelThinkingLevelMinimal,
		ai.ModelThinkingLevelLow,
		ai.ModelThinkingLevelMedium,
		ai.ModelThinkingLevelHigh,
		ai.ModelThinkingLevelXHigh,
	} {
		if supportedLevels[level] {
			if level == ai.ModelThinkingLevelXHigh {
				mapped := string(level)
				out[level] = &mapped
			}
			continue
		}
		out[level] = nil
	}
	return out
}

func (entry aiServicesModelEntry) defaultThinkingLevel() ai.ModelThinkingLevel {
	if entry.Capabilities == nil || entry.Capabilities.Reasoning == nil {
		return ""
	}
	level := ai.ModelThinkingLevel(strings.ToLower(strings.TrimSpace(entry.Capabilities.Reasoning.DefaultLevel)))
	if !modelThinkingLevelKnown(level) {
		return ""
	}
	return level
}

func modelThinkingLevelKnown(level ai.ModelThinkingLevel) bool {
	switch level {
	case ai.ModelThinkingLevelOff, ai.ModelThinkingLevelMinimal, ai.ModelThinkingLevelLow, ai.ModelThinkingLevelMedium, ai.ModelThinkingLevelHigh, ai.ModelThinkingLevelXHigh:
		return true
	default:
		return false
	}
}

func modelContact(provider aiid.ProviderConfig, model ai.Model) *bridgev2.ResolveIdentifierResponse {
	info := modelUserInfo(provider, model)
	return &bridgev2.ResolveIdentifierResponse{
		UserID:   aiid.ModelContactID(provider.ID, model.ID),
		UserInfo: info,
	}
}

func modelUserInfo(provider aiid.ProviderConfig, model ai.Model) *bridgev2.UserInfo {
	name := modelDisplayName(provider, model)
	isBot := true
	return &bridgev2.UserInfo{
		Name:        &name,
		IsBot:       &isBot,
		Identifiers: aiid.ModelContactIdentifiers(provider.ID, model.ID),
		Avatar:      modelAvatar(provider, model),
	}
}

func modelContactWithGhost(ctx context.Context, br *bridgev2.Bridge, provider aiid.ProviderConfig, model ai.Model) *bridgev2.ResolveIdentifierResponse {
	resp := modelContact(provider, model)
	if ghost, err := updateModelGhostInfo(ctx, br, provider, model); err == nil {
		resp.Ghost = ghost
	}
	return resp
}

func updateModelGhostInfo(ctx context.Context, br *bridgev2.Bridge, provider aiid.ProviderConfig, model ai.Model) (*bridgev2.Ghost, error) {
	if br == nil {
		return nil, fmt.Errorf("missing bridge")
	}
	ghost, err := br.GetGhostByID(ctx, aiid.ModelContactID(provider.ID, model.ID))
	if err != nil {
		return nil, err
	}
	ghost.UpdateInfo(ctx, modelUserInfo(provider, model))
	return ghost, nil
}

func resolveModelForProvider(provider aiid.ProviderConfig, identifier string) (ai.Model, bool) {
	if providerID, modelID, ok := aiid.ParseModelContactID(aiidNetworkID(identifier)); ok {
		if providerID != provider.ID {
			return ai.Model{}, false
		}
		for _, model := range contactModels(provider) {
			if model.ID == modelID {
				return model, true
			}
		}
		return ai.Model{}, false
	}
	for _, model := range contactModels(provider) {
		if aiid.MatchesModelIdentifier(provider.ID, model.ID, identifier) {
			return model, true
		}
	}
	return ai.Model{}, false
}

func (cl *Client) resolveModelIdentifier(ctx context.Context, identifier string) (aiid.ProviderConfig, ai.Model, bool) {
	providers := cl.providers()
	if len(providers) == 0 {
		return aiid.ProviderConfig{}, ai.Model{}, false
	}
	for _, provider := range providers {
		var providerOK bool
		provider, providerOK = cl.providerForModelContacts(ctx, provider, false)
		if !providerOK {
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
	return meta
}

func (cl *Client) providers() map[string]aiid.ProviderConfig {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil {
		return nil
	}
	return cl.Main.providersForLogin(cl.UserLogin)
}

func contactModels(provider aiid.ProviderConfig) []ai.Model {
	if len(provider.Models) > 0 {
		return provider.Models
	}
	if provider.DefaultModel == "" {
		return nil
	}
	return []ai.Model{normalizeProviderModel(modelForProviderConfig(provider, provider.DefaultModel), provider)}
}

func cloneModelContacts(contacts []*bridgev2.ResolveIdentifierResponse) []*bridgev2.ResolveIdentifierResponse {
	if contacts == nil {
		return nil
	}
	out := make([]*bridgev2.ResolveIdentifierResponse, 0, len(contacts))
	for _, contact := range contacts {
		if contact == nil {
			out = append(out, nil)
			continue
		}
		cloned := *contact
		if contact.UserInfo != nil {
			userInfo := *contact.UserInfo
			userInfo.Identifiers = slices.Clone(contact.UserInfo.Identifiers)
			cloned.UserInfo = &userInfo
		}
		if contact.Ghost != nil {
			ghost := *contact.Ghost
			cloned.Ghost = &ghost
		}
		out = append(out, &cloned)
	}
	return out
}

func cloneModels(models []ai.Model) []ai.Model {
	if models == nil {
		return nil
	}
	out := make([]ai.Model, len(models))
	for i, model := range models {
		out[i] = model
		out[i].Input = slices.Clone(model.Input)
		out[i].Output = slices.Clone(model.Output)
		out[i].BuiltInTools = slices.Clone(model.BuiltInTools)
		out[i].Headers = maps.Clone(model.Headers)
		out[i].Compat = maps.Clone(model.Compat)
		if model.ThinkingLevelMap != nil {
			out[i].ThinkingLevelMap = make(map[ai.ModelThinkingLevel]*string, len(model.ThinkingLevelMap))
			for level, mapped := range model.ThinkingLevelMap {
				if mapped == nil {
					out[i].ThinkingLevelMap[level] = nil
					continue
				}
				mappedCopy := *mapped
				out[i].ThinkingLevelMap[level] = &mappedCopy
			}
		}
	}
	return out
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
