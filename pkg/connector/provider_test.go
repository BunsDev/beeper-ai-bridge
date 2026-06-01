package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aidb"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

func TestModelForProviderConstructsCustomModel(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       "local",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.Provider("local"),
		BaseURL:  "https://local.test/v1/responses",
	}
	model := conn.ModelForProvider(provider, "local-model")
	if model.ID != "local-model" || model.Provider != "local" || model.API != ai.ApiOpenAIResponses {
		t.Fatalf("unexpected model %#v", model)
	}
	if model.BaseURL != "https://local.test/v1" {
		t.Fatalf("unexpected model base URL %q", model.BaseURL)
	}
	if !isImageModel(model) {
		t.Fatalf("expected custom provider fallback model to support image input")
	}
}

func TestModelForProviderBuildsDefaultProviderModelFromConfig(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  "https://ai-services.beeper-staging.com",
	}
	model := conn.ModelForProvider(provider, "openai/gpt-5.5")
	if model.ID != "openai/gpt-5.5" || model.Provider != ai.ProviderOpenAI || model.API != ai.ApiOpenAIResponses {
		t.Fatalf("unexpected model %#v", model)
	}
	if model.Reasoning || model.ContextWindow != 128000 || model.MaxTokens != 32000 {
		t.Fatalf("expected config-derived model metadata, got %#v", model)
	}
}

func TestModelForProviderPassesCustomOpenAIProviderModelIDDirectly(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       "custom-openai",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  "https://custom.test/v1",
	}
	model := conn.ModelForProvider(provider, "openai/gpt-5.5")
	if model.ID != "openai/gpt-5.5" {
		t.Fatalf("expected custom provider model ID to pass through, got %#v", model)
	}
}

func TestModelForProviderFillsCustomListedModelInputFromCatalog(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       "custom-openai",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  "https://custom.test/v1",
		Models:   []ai.Model{{ID: "gpt-5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses, Input: []string{"text", "image"}}},
	}
	model := conn.ModelForProvider(provider, "gpt-5.5")
	if !isImageModel(model) {
		t.Fatalf("expected catalog-backed GPT-5.5 to support image input, got %#v", model.Input)
	}
}

func TestModelForProviderFillsCustomPrefixedOpenAIInputFromCatalog(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       "custom-openai",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  "https://custom.test/v1",
		Models:   []ai.Model{{ID: "openai/gpt-5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses, Input: []string{"text", "image"}}},
	}
	model := conn.ModelForProvider(provider, "openai/gpt-5.5")
	if !isImageModel(model) {
		t.Fatalf("expected prefixed catalog-backed GPT-5.5 to support image input, got %#v", model.Input)
	}
}

func TestModelForProviderPreservesNonOpenAIBeeperProviderModelID(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenRouter,
		BaseURL:  "https://openrouter.ai/api/v1",
	}
	model := conn.ModelForProvider(provider, "openai/gpt-5")
	if model.ID != "openai/gpt-5" || model.Provider != ai.ProviderOpenRouter {
		t.Fatalf("expected non-OpenAI Beeper provider model ID to stay qualified, got %#v", model)
	}
}

func TestModelForProviderResolvesCatalogModelAlias(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		Models:   []ai.Model{{ID: "openai/gpt-5-mini", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses, Input: []string{"text", "image"}}},
	}
	model := conn.ModelForProvider(provider, "gpt-5-mini")
	if model.ID != "openai/gpt-5-mini" || !isImageModel(model) {
		t.Fatalf("expected catalog model alias to resolve, got %#v", model)
	}
}

func TestTitleGenerationModelUsesDefaultMini(t *testing.T) {
	conn := &Connector{}
	client := &Client{Main: conn}
	provider := conn.defaultProviderConfig("@alice:beeper-staging.com")
	model := client.titleGenerationModel(provider, ai.Model{ID: defaultBeeperAIModel})
	if model.ID != defaultBeeperAIModel {
		t.Fatalf("unexpected title model %#v", model)
	}
}

func TestTitleGenerationModelUsesCatalogResolvedDefaultMini(t *testing.T) {
	client := &Client{Main: &Connector{}}
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		Models:   []ai.Model{{ID: "openai/gpt-4.1-mini", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses, Input: []string{"text", "image"}}},
	}
	model := client.titleGenerationModel(provider, ai.Model{ID: defaultBeeperAIModel})
	if model.ID != "openai/gpt-4.1-mini" || model.Provider != ai.ProviderOpenAI || model.API != ai.ApiOpenAIResponses || model.Reasoning {
		t.Fatalf("unexpected title model %#v", model)
	}
}

func TestTitleGenerationModelFallsBackToGPT5Mini(t *testing.T) {
	client := &Client{Main: &Connector{}}
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		Models:   []ai.Model{{ID: "openai/gpt-5-mini", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses}},
	}
	model := client.titleGenerationModel(provider, ai.Model{ID: defaultBeeperAIModel})
	if model.ID != "openai/gpt-5-mini" || model.Provider != ai.ProviderOpenAI || model.API != ai.ApiOpenAIResponses {
		t.Fatalf("unexpected title model %#v", model)
	}
}

func TestTitleGenerationModelUsesOpenRouterMini(t *testing.T) {
	client := &Client{Main: &Connector{}}
	provider := aiid.ProviderConfig{
		ID:       "openrouter",
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenRouter,
		BaseURL:  "https://openrouter.ai/api/v1",
		Models:   []ai.Model{{ID: openRouterTitleGenerationModel}},
	}
	model := client.titleGenerationModel(provider, ai.Model{ID: "anthropic/claude-sonnet-4.5"})
	if model.ID != openRouterTitleGenerationModel || model.Provider != ai.ProviderOpenRouter || model.API != ai.ApiOpenAICompletions {
		t.Fatalf("unexpected title model %#v", model)
	}
}

func TestTitleGenerationModelAllowsProviderConfiguredModels(t *testing.T) {
	client := &Client{Main: &Connector{}}
	fallback := ai.Model{ID: "local-model", Provider: ai.Provider("local")}
	provider := aiid.ProviderConfig{
		ID:       "local",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		Models:   []ai.Model{{ID: defaultTitleGenerationModel}, fallback},
	}
	model := client.titleGenerationModel(provider, fallback)
	if model.ID != defaultTitleGenerationModel || model.Provider != ai.ProviderOpenAI {
		t.Fatalf("expected config-derived title model, got %#v", model)
	}
}

func TestDefaultProviderAuthUsesAppServiceToken(t *testing.T) {
	client := &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			UserMXID: "@qatest9033045029:beeper-staging.com",
		}},
	}
	auth, err := client.authForProvider(aiid.ProviderConfig{ID: aiid.DefaultProvider})(context.Background(), ai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := strings.CutPrefix(auth.APIKey, aiServicesAppserviceTokenPrefix)
	if !ok {
		t.Fatalf("expected appservice bearer token, got %q", auth.APIKey)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	var token aiServicesAppserviceToken
	if err = json.Unmarshal(decoded, &token); err != nil {
		t.Fatal(err)
	}
	if token.ASToken != "as-token" || token.Username != "qatest9033045029" {
		t.Fatalf("unexpected appservice token %#v", token)
	}
	if len(auth.Headers) != 0 {
		t.Fatalf("expected no extra headers, got %#v", auth.Headers)
	}
}

func TestDefaultProviderAuthRequiresBeeperUsername(t *testing.T) {
	client := &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
	}
	_, err := client.authForProvider(aiid.ProviderConfig{ID: aiid.DefaultProvider})(context.Background(), ai.Model{})
	if err == nil || !strings.Contains(err.Error(), "user login") {
		t.Fatalf("expected user login error, got %v", err)
	}
}

func TestCustomProviderAuthUsesProviderKeyAndHeaders(t *testing.T) {
	client := &Client{Main: &Connector{}}
	auth, err := client.authForProvider(aiid.ProviderConfig{
		ID:      "custom",
		APIKey:  "custom-key",
		Headers: map[string]string{"X-Test": "ok"},
	})(context.Background(), ai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	if auth.APIKey != "custom-key" || auth.Headers["X-Test"] != "ok" {
		t.Fatalf("unexpected auth %#v", auth)
	}
}

func TestConfigDefaults(t *testing.T) {
	config := Config{}
	config.ApplyDefaults()
	if config.DefaultSystemPrompt == "" || config.DefaultReasoningLevel != "off" {
		t.Fatalf("expected chat defaults, got %#v", config)
	}
	if config.Fetch.TimeoutMS == 0 || config.Fetch.MaxBytes == 0 || config.Fetch.MaxChars == 0 {
		t.Fatalf("expected fetch defaults, got %#v", config.Fetch)
	}
}

func TestDefaultAIServicesBaseURLUsesUserHomeserver(t *testing.T) {
	conn := &Connector{}
	tests := map[string]string{
		"@alice:beeper.localtest.me": "https://ai-services.beeper.localtest.me",
		"@alice:beeper-dev.com":      "https://ai-services.beeper-dev.com",
		"@alice:beeper-staging.com":  "https://ai-services.beeper-staging.com",
		"@alice:beeper.com":          "https://ai-services.beeper.com",
	}
	for userMXID, want := range tests {
		if got := conn.defaultAIServicesBaseURL(id.UserID(userMXID)); got != want {
			t.Fatalf("defaultAIServicesBaseURL(%q) = %q, want %q", userMXID, got, want)
		}
	}
}

func TestDefaultProviderBaseURLUsesUserHomeserver(t *testing.T) {
	conn := &Connector{}
	provider := conn.defaultProviderConfig("@alice:beeper-staging.com")
	if provider.BaseURL != "https://ai-services.beeper-staging.com" {
		t.Fatalf("unexpected provider base URL %q", provider.BaseURL)
	}
}

func TestDefaultProviderBaseURLUsesInternalLocalServiceForLocalUser(t *testing.T) {
	conn := &Connector{HomeserverURL: "http://megahungry-proxy.megahungry/api/proxy/bridge-user"}
	provider := conn.defaultProviderConfig("@alice:beeper.localtest.me")
	if provider.BaseURL != "http://ai-services.beeper" {
		t.Fatalf("unexpected provider base URL %q", provider.BaseURL)
	}
}

func TestDefaultProviderBaseURLUsesUserHomeserverForMegahungryCloudUser(t *testing.T) {
	conn := &Connector{HomeserverURL: "http://megahungry-proxy.megahungry/api/proxy/bridge-user"}
	provider := conn.defaultProviderConfig("@alice:beeper-staging.com")
	if provider.BaseURL != "https://ai-services.beeper-staging.com" {
		t.Fatalf("unexpected provider base URL %q", provider.BaseURL)
	}
}

func TestDefaultProviderBaseURLUsesExternalLocaltestServiceForSelfHosted(t *testing.T) {
	conn := &Connector{HomeserverURL: "https://matrix.beeper.localtest.me/_hungryserv/bridge-user"}
	provider := conn.defaultProviderConfig("@alice:beeper.localtest.me")
	if provider.BaseURL != "https://ai-services.beeper.localtest.me" {
		t.Fatalf("unexpected provider base URL %q", provider.BaseURL)
	}
}

func TestDefaultProviderBaseURLRejectsUserHomeserverMismatch(t *testing.T) {
	conn := &Connector{HomeserverURL: "https://matrix.beeper.com/_hungryserv/bridge-user"}
	provider := conn.defaultProviderConfig("@alice:evil.example")
	if provider.BaseURL != "" {
		t.Fatalf("expected no default provider route for mismatched user homeserver, got %q", provider.BaseURL)
	}
}

func TestDefaultProviderReadsAIChatsLoginMetadata(t *testing.T) {
	conn := &Connector{}
	provider := conn.defaultProviderConfig("@alice:beeper.localtest.me")
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		UserMXID: "@alice:beeper.localtest.me",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}
	got := conn.providersForLogin(login)[aiid.DefaultProvider]
	if got.BaseURL != "https://ai-services.beeper.localtest.me" {
		t.Fatalf("expected persisted default provider, got %#v", got)
	}
}

func TestProvidersForLoginReadsProviderMap(t *testing.T) {
	conn := &Connector{}
	beeper := conn.defaultProviderConfig("@alice:beeper.com")
	custom := aiid.ProviderConfig{
		ID:           "custom",
		DisplayName:  "Custom",
		API:          ai.ApiOpenAIResponses,
		Provider:     "custom",
		BaseURL:      "https://example.test/v1",
		APIKey:       "secret-key",
		DefaultModel: "model-a",
		Models:       []ai.Model{{ID: "model-a"}},
	}
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		UserMXID: "@alice:beeper.com",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
			beeper.ID: beeper,
			custom.ID: custom,
		}},
	}}
	providers := conn.providersForLogin(login)
	if providers[aiid.DefaultProvider].BaseURL != "https://ai-services.beeper.com" {
		t.Fatalf("default provider missing from provider map: %#v", providers)
	}
	if providers["custom"].APIKey != "secret-key" || providers["custom"].DefaultModel != "model-a" {
		t.Fatalf("custom provider missing from provider map: %#v", providers)
	}
}

func TestLoadUserLoginAlwaysCreatesAIClient(t *testing.T) {
	conn := &Connector{}
	userMXID := id.UserID("@alice:beeper.com")
	provider := conn.defaultProviderConfig(userMXID)
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       conn.defaultLoginID(userMXID),
		UserMXID: userMXID,
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
			provider.ID: provider,
		}},
	}}
	if err := conn.LoadUserLogin(context.Background(), login); err != nil {
		t.Fatal(err)
	}
	if _, ok := login.Client.(*Client); !ok {
		t.Fatalf("expected AI client for login, got %T", login.Client)
	}
}

func TestLoadUserLoginDoesNotRecoverPersistedActiveStreams(t *testing.T) {
	ctx := context.Background()
	store := testAIStore(t)
	conn := &Connector{Store: store}
	userMXID := id.UserID("@alice:beeper.com")
	loginID := conn.defaultLoginID(userMXID)
	provider := conn.defaultProviderConfig(userMXID)
	run := aistream.NewRun("run-1", "session-1", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	writer.Start()
	if err := store.UpsertActiveStream(ctx, aidb.ActiveStreamRecord{
		RunID:      run.RunID,
		LoginID:    loginID,
		PortalKey:  networkid.PortalKey{ID: "chat:1", Receiver: loginID},
		RoomID:     "!room:beeper.com",
		EventID:    "$event",
		MessageID:  "assistant:run",
		ProviderID: "beeper",
		ModelID:    "fake",
		Run:        *run,
	}); err != nil {
		t.Fatal(err)
	}
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       loginID,
		UserMXID: userMXID,
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
			provider.ID: provider,
		}},
	}}
	if err := conn.LoadUserLogin(ctx, login); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListActiveStreams(ctx, loginID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RunID != run.RunID {
		t.Fatalf("LoadUserLogin should not recover active streams, got %#v", records)
	}
}

func TestProviderResponseRedactsSecrets(t *testing.T) {
	response := providerResponse(aiid.ProviderConfig{
		ID:           "custom",
		DisplayName:  "Custom",
		API:          ai.ApiOpenAIResponses,
		Provider:     "custom",
		BaseURL:      "https://example.test/v1",
		APIKey:       "secret-key",
		RefreshToken: "refresh-secret",
		Headers:      map[string]string{"Authorization": "Bearer header-secret"},
		DefaultModel: "model-a",
	})
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"secret-key", "refresh-secret", "header-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("provider response leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "model-a") {
		t.Fatalf("provider response lost public fields: %s", text)
	}
}

func TestDefaultProviderHasNoRouteWithoutUserHomeserver(t *testing.T) {
	conn := &Connector{}
	provider := conn.defaultProviderConfig("")
	if provider.BaseURL != "" {
		t.Fatalf("expected provider without user homeserver to have no route, got %q", provider.BaseURL)
	}
}

func TestConnectorCapabilitiesAdvertiseAISessionCreation(t *testing.T) {
	conn := &Connector{}
	caps := conn.GetCapabilities()
	if _, ok := caps.Provisioning.GroupCreation["ai"]; !ok {
		t.Fatalf("expected AI group creation capability, got %#v", caps.Provisioning.GroupCreation)
	}
	if !caps.Provisioning.ResolveIdentifier.ContactList || !caps.Provisioning.ResolveIdentifier.Search || !caps.Provisioning.ResolveIdentifier.CreateDM {
		t.Fatalf("expected contact list/search/create DM capabilities, got %#v", caps.Provisioning.ResolveIdentifier)
	}
	groupCaps := caps.Provisioning.GroupCreation["ai"]
	if !groupCaps.Disappear.Allowed || groupCaps.Disappear.DisappearSettings == nil {
		t.Fatalf("expected initial disappearing timer support, got %#v", groupCaps.Disappear)
	}
	_, capVersion := conn.GetBridgeInfoVersion()
	if capVersion < 6 {
		t.Fatalf("capability version must bump when provisioning capabilities change, got %d", capVersion)
	}
}

func TestNoAIChatStatusIsLocalAndPermanent(t *testing.T) {
	status := errNoAIChat()
	if status.Status != event.MessageStatusFail || status.ErrorReason != event.MessageStatusUnsupported {
		t.Fatalf("expected permanent unsupported no-portal status, got %#v", status)
	}
	if status.Message == "" || status.ErrorReason == event.MessageStatusGenericError {
		t.Fatalf("expected specific no-portal message status, got %#v", status)
	}
	if status.InternalError == nil || status.InternalError.Error() != "room is not an AI chat" {
		t.Fatalf("expected local no-AI-chat internal error, got %#v", status.InternalError)
	}
}

func TestRoomFeaturesDisableReactions(t *testing.T) {
	client := &Client{}
	caps := client.GetCapabilities(context.Background(), nil)
	if caps.ID != "" {
		if caps.GetID() != caps.ID {
			t.Fatalf("expected explicit capability ID to be used as dedupe ID, got id=%q dedupe=%q", caps.ID, caps.GetID())
		}
	} else if caps.GetID() == "" {
		t.Fatalf("expected room features to expose an ID")
	}
	if caps.Reaction != event.CapLevelUnsupported {
		t.Fatalf("expected reactions to be unsupported, got %d", caps.Reaction)
	}
	if caps.State[event.StateRoomName.Type] == nil || caps.State[event.StateRoomName.Type].Level != event.CapLevelFullySupported {
		t.Fatalf("expected room name support, got %#v", caps.State[event.StateRoomName.Type])
	}
	if caps.State[event.StateTopic.Type] == nil || caps.State[event.StateTopic.Type].Level != event.CapLevelFullySupported {
		t.Fatalf("expected room topic support, got %#v", caps.State[event.StateTopic.Type])
	}
	if caps.State[event.StateBeeperDisappearingTimer.Type] == nil || caps.State[event.StateBeeperDisappearingTimer.Type].Level != event.CapLevelFullySupported {
		t.Fatalf("expected disappearing timer support, got %#v", caps.State[event.StateBeeperDisappearingTimer.Type])
	}
	if !caps.DeleteChat {
		t.Fatalf("expected delete chat support")
	}
	if caps.TypingNotifications {
		t.Fatalf("did not expect Matrix typing notifications to be advertised")
	}
	if caps.File[event.MsgImage] != nil {
		t.Fatalf("did not expect image support without a vision model")
	}
	if caps.File[event.MsgAudio] != nil || caps.File[event.CapMsgVoice] != nil {
		t.Fatalf("did not expect audio support without a native audio model")
	}
	if caps.File[event.MsgFile] == nil || caps.File[event.MsgFile].MimeTypes["text/*"] != event.CapLevelFullySupported {
		t.Fatalf("expected text file support, got %#v", caps.File[event.MsgFile])
	}
	if caps.LocationMessage != event.CapLevelFullySupported {
		t.Fatalf("expected location message support, got %d", caps.LocationMessage)
	}
	for _, mimeType := range []string{"text/calendar", "application/csv", "application/x-subrip"} {
		if caps.File[event.MsgFile].MimeTypes[mimeType] != event.CapLevelFullySupported {
			t.Fatalf("expected %s file support, got %#v", mimeType, caps.File[event.MsgFile])
		}
	}
	for feature, level := range caps.Formatting {
		if level != event.CapLevelFullySupported {
			t.Fatalf("expected %s formatting to be fully supported, got %d", feature, level)
		}
	}
	for _, feature := range []event.FormattingFeature{
		event.FmtBold,
		event.FmtUnderline,
		event.FmtSyntaxHighlighting,
		event.FmtInlineLink,
		event.FmtUserLink,
		event.FmtSpoiler,
		event.FmtTextForegroundColor,
		event.FmtHeaders,
		event.FmtTable,
	} {
		if caps.Formatting[feature] != event.CapLevelFullySupported {
			t.Fatalf("expected %s formatting support, got %d", feature, caps.Formatting[feature])
		}
	}
	for _, stateType := range []string{aiid.RoomToolsType, aiid.RoomModelType, aiid.RoomPromptType} {
		if caps.State[stateType] != nil {
			t.Fatalf("did not expect %s state support without arbitrary state support, got %#v", stateType, caps.State[stateType])
		}
	}
	withState := roomFeaturesForModel(ai.Model{}, true)
	for _, stateType := range []string{aiid.RoomToolsType, aiid.RoomModelType, aiid.RoomPromptType} {
		if withState.State[stateType] == nil || withState.State[stateType].Level != event.CapLevelFullySupported {
			t.Fatalf("expected %s state support with arbitrary state support, got %#v", stateType, withState.State[stateType])
		}
	}
}

func TestRoomFeaturesFollowModelInputModalities(t *testing.T) {
	textCaps := roomFeaturesForModel(ai.Model{Input: []string{"text"}}, true)
	if textCaps.File[event.MsgImage] != nil || textCaps.File[event.MsgAudio] != nil || textCaps.File[event.CapMsgVoice] != nil {
		t.Fatalf("text-only model should not advertise media input, got %#v", textCaps.File)
	}
	if textCaps.File[event.MsgFile] == nil {
		t.Fatalf("text-like files should be available through prompt text conversion")
	}

	visionCaps := roomFeaturesForModel(ai.Model{Input: []string{"text", "image"}}, true)
	if visionCaps.File[event.MsgImage] == nil {
		t.Fatalf("vision model should advertise image input")
	}
	if visionCaps.File[event.CapMsgGIF] != nil || visionCaps.File[event.MsgImage].MimeTypes["image/gif"] != event.CapLevelUnsupported {
		t.Fatalf("vision model should not advertise GIF input, got gif=%#v image/gif=%d", visionCaps.File[event.CapMsgGIF], visionCaps.File[event.MsgImage].MimeTypes["image/gif"])
	}
	if visionCaps.File[event.MsgAudio] != nil {
		t.Fatalf("vision-only model should not advertise audio input")
	}

	audioCaps := roomFeaturesForModel(ai.Model{Input: []string{"text", "audio"}}, true)
	if audioCaps.File[event.MsgAudio] == nil || audioCaps.File[event.CapMsgVoice] == nil {
		t.Fatalf("audio model should advertise audio and voice input")
	}
	if audioCaps.File[event.MsgAudio].MimeTypes["audio/wav"] != event.CapLevelFullySupported ||
		audioCaps.File[event.MsgAudio].MimeTypes["audio/mpeg"] != event.CapLevelFullySupported {
		t.Fatalf("audio model should advertise native wav/mp3 support, got %#v", audioCaps.File[event.MsgAudio])
	}
	if audioCaps.File[event.MsgAudio].Caption != event.CapLevelFullySupported {
		t.Fatalf("audio model should advertise caption support, got %#v", audioCaps.File[event.MsgAudio])
	}
}

func TestRoomFeaturesUseDeterministicIDs(t *testing.T) {
	first := roomFeaturesForModel(ai.Model{ID: "model-a", Input: []string{"text", "image"}}, true)
	second := roomFeaturesForModel(ai.Model{ID: "model-b", Input: []string{"text", "image"}}, true)
	if first.ID != "com.beeper.ai.capabilities.2026_06_01.no_typing+state+image" {
		t.Fatalf("expected deterministic image capability ID, got %q", first.ID)
	}
	if first.ID != second.ID {
		t.Fatalf("same feature values should produce same ID, got %q and %q", first.ID, second.ID)
	}
	textOnly := roomFeaturesForModel(ai.Model{Input: []string{"text"}}, true)
	if textOnly.ID != "com.beeper.ai.capabilities.2026_06_01.no_typing+state" {
		t.Fatalf("expected deterministic text capability ID, got %q", textOnly.ID)
	}
	withoutState := roomFeaturesForModel(ai.Model{ID: "model-a", Input: []string{"text", "image"}}, false)
	if withoutState.ID != "com.beeper.ai.capabilities.2026_06_01.no_typing+image" {
		t.Fatalf("expected deterministic image capability ID without AI state, got %q", withoutState.ID)
	}
}

func TestUnsupportedPromptAudioAttachment(t *testing.T) {
	if unsupported := unsupportedPromptAudioAttachment([]ai.ContentBlock{{Type: "audio", MimeType: "audio/mpeg"}}); unsupported != "" {
		t.Fatalf("expected mp3 audio to be accepted, got %q", unsupported)
	}
	if unsupported := unsupportedPromptAudioAttachment([]ai.ContentBlock{{Type: "audio", MimeType: "audio/ogg"}}); unsupported != "audio/ogg" {
		t.Fatalf("expected ogg audio to be rejected for native pass-through, got %q", unsupported)
	}
	if unsupported := unsupportedPromptAudioAttachment([]ai.ContentBlock{{Type: "image", MimeType: "audio/ogg"}}); unsupported != "" {
		t.Fatalf("expected non-audio block to be ignored, got %q", unsupported)
	}
}

func TestProviderModelsParsesOptionalModelList(t *testing.T) {
	models := providerModels("gpt-5-mini, gpt-5\nlocal", "gpt-5", "custom", "https://example.test/v1")
	if len(models) != 3 {
		t.Fatalf("expected default plus two unique models, got %#v", models)
	}
	if models[0].ID != "gpt-5" || models[1].ID != "gpt-5-mini" || models[2].ID != "local" {
		t.Fatalf("unexpected model order %#v", models)
	}
}

func TestCatalogRuntimeForCustomProviderUsesCustomBaseURL(t *testing.T) {
	entry := aiServicesModelEntry{
		ID:   "z-ai/glm-4.5v",
		Name: "GLM 4.5V",
		Runtime: &struct {
			API      string                 `json:"api"`
			Provider string                 `json:"provider"`
			BaseURL  string                 `json:"base_url"`
			Model    string                 `json:"model"`
			Endpoint string                 `json:"endpoint"`
			Compat   *aiServicesModelCompat `json:"compat"`
		}{
			API:      string(ai.ApiOpenAICompletions),
			Provider: string(ai.ProviderOpenRouter),
			BaseURL:  "/proxy/openrouter/v1",
			Model:    "z-ai/glm-4.5v",
		},
	}
	provider := aiid.ProviderConfig{
		ID:       "openrouter",
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenRouter,
		BaseURL:  "https://openrouter.ai/api/v1",
	}
	model := entry.applyRuntime(ai.Model{
		ID:       entry.ID,
		Name:     entry.Name,
		API:      provider.API,
		Provider: provider.Provider,
		BaseURL:  provider.BaseURL,
	}, provider, false)
	if model.API != ai.ApiOpenAICompletions || model.Provider != ai.ProviderOpenRouter || model.BaseURL != provider.BaseURL {
		t.Fatalf("unexpected custom provider runtime %#v", model)
	}
	if model.Compat["runtime_model"] != "z-ai/glm-4.5v" || model.Compat["runtime_provider"] != string(ai.ProviderOpenRouter) {
		t.Fatalf("expected runtime metadata, got %#v", model.Compat)
	}
}

func TestCustomProviderLoginConfigStep(t *testing.T) {
	login := &CustomProviderLogin{config: providerLoginConfig{API: ai.ApiOpenAICompletions}}
	step := login.providerConfigStep()
	if step.StepID != loginStepProviderConfig || len(step.UserInputParams.Fields) != 3 {
		t.Fatalf("unexpected config step %#v", step)
	}
}

func TestModelForProviderAppliesRouteBaseURLToDefaultModel(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  "https://ai-services.beeper.com",
	}
	model := conn.ModelForProvider(provider, "gpt-5.5")
	if model.Provider != ai.ProviderOpenAI || model.ID != "gpt-5.5" {
		t.Fatalf("expected AI Services model, got %#v", model)
	}
	if model.BaseURL != "https://ai-services.beeper.com" {
		t.Fatalf("expected route base URL override, got %q", model.BaseURL)
	}
	if model.ContextWindow != 128000 || model.MaxTokens != 32000 {
		t.Fatalf("expected config-derived model metadata, got %#v", model)
	}
}

func TestResolveProviderDefersModelValidationToCatalogLoad(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:           "custom",
		Provider:     "custom",
		API:          ai.ApiOpenAIResponses,
		DefaultModel: "allowed",
		Models:       []ai.Model{{ID: "allowed", Provider: "custom", API: ai.ApiOpenAIResponses}},
	}
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}
	_, modelID, err := conn.ResolveProvider(context.Background(), login, RoomConfig{ProviderID: "custom", ModelID: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if modelID != "missing" {
		t.Fatalf("unexpected model ID %q", modelID)
	}
	_, modelID, err = conn.ResolveProvider(context.Background(), login, RoomConfig{ProviderID: "custom", ModelID: "allowed"})
	if err != nil {
		t.Fatal(err)
	}
	if modelID != "allowed" {
		t.Fatalf("unexpected model ID %q", modelID)
	}
}

func TestValidateReasoningLevelRejectsUnsupportedPair(t *testing.T) {
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	model := ai.Model{ID: "plain", Input: []string{"text"}}
	if err := client.validateReasoningLevel(model, RoomConfig{ThinkingLevel: "high"}); err == nil {
		t.Fatalf("expected unsupported reasoning level to fail")
	}
	if err := client.validateReasoningLevel(model, RoomConfig{ThinkingLevel: "off"}); err != nil {
		t.Fatalf("expected off reasoning to be accepted: %v", err)
	}
}

func TestValidateReasoningLevelAcceptsOffForReasoningModel(t *testing.T) {
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	model := ai.Model{ID: "reasoning", Reasoning: true}
	if err := client.validateReasoningLevel(model, RoomConfig{}); err != nil {
		t.Fatalf("expected default off reasoning to be accepted: %v", err)
	}
}

func TestDefaultReasoningLevelClampsForMandatoryReasoningModel(t *testing.T) {
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	model := ai.Model{
		ID:                   "minimax/minimax-m2.7",
		Provider:             ai.ProviderOpenRouter,
		Reasoning:            true,
		DefaultThinkingLevel: ai.ModelThinkingLevelLow,
		ThinkingLevelMap:     map[ai.ModelThinkingLevel]*string{ai.ModelThinkingLevelOff: nil},
	}
	if got := client.reasoningLevelForModel(model, RoomConfig{}); got != "low" {
		t.Fatalf("expected default off to clamp to low, got %q", got)
	}
	if err := client.validateReasoningLevel(model, RoomConfig{}); err != nil {
		t.Fatalf("expected clamped default reasoning to be accepted: %v", err)
	}
	if err := client.validateReasoningLevel(model, RoomConfig{ThinkingLevel: "off"}); err == nil {
		t.Fatalf("expected explicit off reasoning to be rejected")
	}
}

func TestValidateReasoningModeRejectsUnsupportedPair(t *testing.T) {
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	model := ai.Model{ID: "plain", Input: []string{"text"}}
	if err := client.validateReasoningMode(model, RoomConfig{ReasoningMode: "adaptive"}); err == nil {
		t.Fatalf("expected unsupported reasoning mode to fail")
	}
	if err := client.validateReasoningMode(model, RoomConfig{ReasoningMode: "default"}); err != nil {
		t.Fatalf("expected default reasoning mode to be accepted: %v", err)
	}
	if err := client.validateReasoningMode(model, RoomConfig{ReasoningMode: "bad"}); err == nil {
		t.Fatalf("expected invalid reasoning mode to fail")
	}
}

func TestReasoningModeDefaultsFromModelCatalog(t *testing.T) {
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	model := ai.Model{ID: "anthropic/claude-opus-4.8", ReasoningMode: ai.ModelReasoningModeAdaptive}
	if got := client.reasoningModeForModel(model, RoomConfig{}); got != "adaptive" {
		t.Fatalf("expected catalog reasoning mode, got %q", got)
	}
	if err := client.validateReasoningMode(model, RoomConfig{ReasoningMode: "adaptive"}); err != nil {
		t.Fatalf("expected adaptive reasoning mode to be accepted: %v", err)
	}
}

func TestNormalizeProviderModelDoesNotInheritDefaultProviderCatalogMetadata(t *testing.T) {
	model := normalizeProviderModel(ai.Model{
		ID:       "minimax/minimax-m2.7",
		Provider: ai.ProviderOpenRouter,
	}, aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		BaseURL:  "https://ai-services.test",
	})
	if model.Reasoning || model.DefaultThinkingLevel != "" || len(model.ThinkingLevelMap) != 0 {
		t.Fatalf("expected default provider model to rely only on AI Services metadata, got %#v", model)
	}
	if len(model.Input) != 1 || model.Input[0] != "text" {
		t.Fatalf("expected default provider missing input to fall back to text only, got %#v", model.Input)
	}
}

func TestNormalizeProviderModelInheritsCustomProviderCatalogReasoningMetadata(t *testing.T) {
	model := normalizeProviderModel(ai.Model{
		ID:        "gpt-5.5",
		Provider:  ai.ProviderOpenAI,
		Reasoning: true,
		ThinkingLevelMap: map[ai.ModelThinkingLevel]*string{
			ai.ModelThinkingLevelOff: nil,
		},
	}, aiid.ProviderConfig{
		ID:       "custom-openai",
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		BaseURL:  "https://custom.test/v1",
	})
	if !model.Reasoning || len(model.ThinkingLevelMap) == 0 {
		t.Fatalf("expected custom provider model to preserve catalog reasoning metadata, got %#v", model)
	}
	if roomThinkingLevelSupported(model, ai.ModelThinkingLevelOff) {
		t.Fatalf("expected normalized GPT-5.5 custom provider model to reject off reasoning")
	}
}
