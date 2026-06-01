package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

func TestModelContactsExposeConfiguredModels(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:          "local",
		DisplayName: "Local",
		Provider:    "local",
		API:         ai.ApiOpenAIResponses,
		Models:      []ai.Model{{ID: "model-a"}, {ID: "model-b", Name: "Model Bee"}},
	}
	client := &Client{Main: &Connector{}, UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}}
	contacts, err := client.GetContactList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 2 {
		t.Fatalf("expected two contacts, got %#v", contacts)
	}
	if contacts[1].UserInfo == nil || contacts[1].UserInfo.Name == nil || *contacts[1].UserInfo.Name != "Model Bee" {
		t.Fatalf("unexpected contact info %#v", contacts[1])
	}
	providerID, modelID, ok := aiid.ParseModelContactID(contacts[0].UserID)
	if !ok || providerID != "local" || modelID != "model-a" {
		t.Fatalf("unexpected model contact ID %q parsed as %q %q", contacts[0].UserID, providerID, modelID)
	}
}

func TestModelContactsCacheRefreshesAfterCachedRead(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"openai/gpt-5.5","name":"GPT-5.5"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"anthropic/claude-sonnet-4.5","name":"Claude Sonnet 4.5"}]}`))
	}))
	defer server.Close()

	client := newDefaultProviderContactClient(server.URL)
	contacts, err := client.GetContactList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].UserID != aiid.ModelContactID(aiid.DefaultProvider, "openai/gpt-5.5") {
		t.Fatalf("expected initial cached GPT contact, got %#v", contacts)
	}
	results, err := client.SearchUsers(context.Background(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected first cached Claude search to miss before refresh, got %#v", results)
	}
	waitForCachedContact(t, client, aiid.ModelContactID(aiid.DefaultProvider, "anthropic/claude-sonnet-4.5"))
	results, err = client.SearchUsers(context.Background(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UserID != aiid.ModelContactID(aiid.DefaultProvider, "anthropic/claude-sonnet-4.5") {
		t.Fatalf("expected next Claude search to use refreshed cache, got %#v", results)
	}
}

func TestConnectWarmsModelContactsCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"openai/gpt-5.5","name":"GPT-5.5"}]}`))
	}))
	defer server.Close()

	client := newDefaultProviderContactClient(server.URL)
	client.Connect(context.Background())
	deadline := time.Now().Add(time.Second)
	for {
		client.contactCacheMu.Lock()
		warmed := client.contactCache.valid
		client.contactCacheMu.Unlock()
		if warmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for model contacts cache warmup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	contacts, err := client.GetContactList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 {
		t.Fatalf("expected one warmed contact, got %#v", contacts)
	}
	if got := requests.Load(); got < 1 {
		t.Fatalf("expected warmup to request AI Services catalog, got %d", got)
	}
}

func waitForCachedContact(t *testing.T, client *Client, userID networkid.UserID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		client.contactCacheMu.Lock()
		contacts := cloneModelContacts(client.contactCache.contacts)
		client.contactCacheMu.Unlock()
		for _, contact := range contacts {
			if contact != nil && contact.UserID == userID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for cached contact %s", userID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newDefaultProviderContactClient(baseURL string) *Client {
	provider := aiid.ProviderConfig{
		ID:           aiid.DefaultProvider,
		DisplayName:  "Beeper AI",
		Provider:     ai.ProviderOpenAI,
		API:          ai.ApiOpenAIResponses,
		BaseURL:      baseURL,
		DefaultModel: "openai/gpt-5.5",
	}
	return &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			ID:       "login",
			UserMXID: "@test:beeper-staging.com",
			Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
				provider.ID: provider,
			}},
		}},
	}
}

func TestAIServicesCatalogModelsFetchesVisibleModels(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("feature") != "bridge:ai" || r.URL.Query().Has("route") {
			t.Fatalf("unexpected query %s", r.URL.RawQuery)
		}
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"openai/gpt-5.5","name":"GPT-5.5","capabilities":{"input":{"modalities":["text","image"]},"output":{"modalities":["text"]},"reasoning":{"supported":true,"levels":["off","minimal","low","medium","high","xhigh"],"level_map":{"xhigh":"xhigh"},"default_level":"off","mode":"adaptive"},"tools":{"supported":true,"built_in":["image_generation"]},"limits":{"context_tokens":1050000,"output_tokens":128000}}},{"id":"minimax/minimax-m2.7","name":"MiniMax M2.7","runtime":{"provider":"openrouter","model":"minimax/minimax-m2.7","api":"openai-completions","base_url":"/proxy/openrouter/v1","compat":{"supports_developer_role":false,"supports_reasoning_effort":true,"max_tokens_field":"max_completion_tokens","thinking_format":"openrouter"}},"capabilities":{"input":{"modalities":["text"]},"output":{"modalities":["text"]},"reasoning":{"supported":true,"levels":["low","medium","high"],"level_map":{"off":null,"minimal":null},"default_level":"low"}}},{"id":"beeper/fast","name":"Beeper Fast","capabilities":{"input":{"modalities":["text"]},"output":{"modalities":["text"]}}}]}`))
	}))
	defer server.Close()

	client := &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			UserMXID: "@test:beeper-staging.com",
		}},
	}
	models, err := client.aiServicesCatalogModels(context.Background(), aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotAuth, "Bearer "+aiServicesAppserviceTokenPrefix) {
		t.Fatalf("unexpected auth header %q", gotAuth)
	}
	if len(models) != 3 || models[0].ID != "openai/gpt-5.5" || models[0].BaseURL != server.URL {
		t.Fatalf("unexpected models %#v", models)
	}
	if models[0].ContextWindow != 1050000 || models[0].MaxTokens != 128000 {
		t.Fatalf("expected AI Services metadata, got %#v", models[0])
	}
	if len(models[0].Output) != 1 || models[0].Output[0] != "text" {
		t.Fatalf("expected AI Services output metadata, got %#v", models[0].Output)
	}
	if !models[0].Reasoning {
		t.Fatalf("expected AI Services reasoning metadata, got %#v", models[0])
	}
	if models[0].DefaultThinkingLevel != ai.ModelThinkingLevelOff || !roomThinkingLevelSupported(models[0], ai.ModelThinkingLevelOff) {
		t.Fatalf("expected AI Services reasoning defaults, got %#v", models[0])
	}
	if got := models[0].ThinkingLevelMap[ai.ModelThinkingLevelXHigh]; got == nil || *got != "xhigh" {
		t.Fatalf("expected AI Services xhigh map, got %#v", models[0].ThinkingLevelMap)
	}
	if models[0].ReasoningMode != "adaptive" {
		t.Fatalf("expected AI Services reasoning mode, got %#v", models[0].ReasoningMode)
	}
	if len(models[0].BuiltInTools) != 1 || models[0].BuiltInTools[0] != "image_generation" {
		t.Fatalf("expected AI Services built-in tools, got %#v", models[0].BuiltInTools)
	}
	if supported, ok := models[0].Compat["tools_supported"].(bool); !ok || !supported {
		t.Fatalf("expected AI Services tool support metadata, got %#v", models[0].Compat)
	}
	if modelSupportsAgentTools(models[1]) {
		t.Fatalf("expected missing AI Services tools capability to disable agent tools, got %#v", models[1].Compat)
	}
	if models[1].DefaultThinkingLevel != ai.ModelThinkingLevelLow || roomThinkingLevelSupported(models[1], ai.ModelThinkingLevelOff) {
		t.Fatalf("expected MiniMax reasoning to default to low and reject off, got %#v", models[1])
	}
	if models[1].API != ai.ApiOpenAICompletions || models[1].Provider != ai.ProviderOpenRouter || models[1].BaseURL != server.URL+"/proxy/openrouter/v1" {
		t.Fatalf("expected MiniMax OpenRouter route, got %#v", models[1])
	}
	if models[1].Compat["supportsDeveloperRole"] != false || models[1].Compat["thinkingFormat"] != "openrouter" || models[1].Compat["maxTokensField"] != "max_completion_tokens" {
		t.Fatalf("expected MiniMax OpenRouter compat from AI Services, got %#v", models[1].Compat)
	}
	if roomThinkingLevelSupported(models[1], ai.ModelThinkingLevelMinimal) {
		t.Fatalf("expected MiniMax reasoning to reject minimal, got %#v", models[1])
	}
}

func TestResolveCanonicalRoomModelUsesAIServicesCatalogBeforeValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"openai/gpt-5.4","name":"GPT-5.4","runtime":{"provider":"openai","model":"gpt-5.4","api":"openai-responses","base_url":"/proxy/openai/v1"},"capabilities":{"input":{"modalities":["text"]},"output":{"modalities":["text"]}}}]}`))
	}))
	defer server.Close()

	provider := aiid.ProviderConfig{
		ID:           aiid.DefaultProvider,
		Provider:     ai.ProviderOpenAI,
		API:          ai.ApiOpenAIResponses,
		BaseURL:      server.URL,
		DefaultModel: "beeper/default",
		Models:       []ai.Model{{ID: "beeper/default", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses}},
	}
	client := &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			ID:       "login",
			UserMXID: "@test:beeper.localtest.me",
			Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
				provider.ID: provider,
			}},
		}},
	}

	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ProviderID: aiid.DefaultProvider, ModelID: "openai/gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "openai/gpt-5.4" || model.BaseURL != server.URL+"/proxy/openai/v1" || canonical != "beeper/openai/gpt-5.4" {
		t.Fatalf("unexpected resolved model canonical=%q model=%#v", canonical, model)
	}
}

func TestAIServicesModelEntryReasoningDefaultsCanRequireReasoning(t *testing.T) {
	var entry aiServicesModelEntry
	if err := json.Unmarshal([]byte(`{"id":"deepseek/deepseek-r1-0528","name":"DeepSeek R1 (0528)","capabilities":{"input":{"modalities":["text"]},"output":{"modalities":["text"]},"reasoning":{"supported":true,"levels":["minimal","low","medium","high"],"level_map":{"off":null},"default_level":"minimal"}}}`), &entry); err != nil {
		t.Fatal(err)
	}
	model := ai.Model{
		ID:                   entry.ID,
		Name:                 entry.Name,
		Reasoning:            entry.reasoning(),
		ThinkingLevelMap:     entry.thinkingLevelMap(),
		DefaultThinkingLevel: entry.defaultThinkingLevel(),
		Input:                entry.inputModalities(),
		Output:               entry.outputModalities(),
	}
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	if got := client.reasoningLevelForModel(model, RoomConfig{}); got != "minimal" {
		t.Fatalf("expected catalog default reasoning, got %q", got)
	}
	if roomThinkingLevelSupported(model, ai.ModelThinkingLevelOff) {
		t.Fatalf("expected off reasoning to be unsupported, got %#v", model.ThinkingLevelMap)
	}
	if err := client.validateReasoningLevel(model, RoomConfig{}); err != nil {
		t.Fatalf("expected catalog default reasoning to validate: %v", err)
	}
	if err := client.validateReasoningLevel(model, RoomConfig{ThinkingLevel: "off"}); err == nil {
		t.Fatalf("expected explicit off reasoning to be rejected")
	}
}

func TestAIServicesCatalogModelsDoNotUseBridgeCatalogWhenMetadataMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"openai/gpt-5.5","name":"GPT-5.5"}]}`))
	}))
	defer server.Close()

	client := &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			UserMXID: "@test:beeper-staging.com",
		}},
	}
	models, err := client.aiServicesCatalogModels(context.Background(), aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected one model, got %#v", models)
	}
	if isImageModel(models[0]) {
		t.Fatalf("expected missing AI Services input metadata to not inherit bridge catalog image support, got %#v", models[0].Input)
	}
	if models[0].Reasoning || models[0].DefaultThinkingLevel != "" || len(models[0].ThinkingLevelMap) != 0 {
		t.Fatalf("expected missing AI Services reasoning metadata to not inherit bridge catalog reasoning support, got %#v", models[0])
	}
	if len(models[0].Input) != 1 || models[0].Input[0] != "text" {
		t.Fatalf("expected missing AI Services input metadata to fall back to text only, got %#v", models[0].Input)
	}
}

func TestAIServicesModelsURLUsesBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://ai-services.beeper.com":      "https://ai-services.beeper.com/models?feature=bridge%3Aai",
		"https://ai-services.beeper.com/":     "https://ai-services.beeper.com/models?feature=bridge%3Aai",
		"https://ai-services.beeper.com/dev":  "https://ai-services.beeper.com/dev/models?feature=bridge%3Aai",
		"https://ai-services.beeper.com/dev/": "https://ai-services.beeper.com/dev/models?feature=bridge%3Aai",
	}
	for input, want := range tests {
		got, err := aiServicesModelsURL(input)
		if err != nil {
			t.Fatalf("aiServicesModelsURL(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("aiServicesModelsURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAIServicesCatalogModelsUsesPublishedProviderRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"claude-sonnet-4-5","name":"Claude Sonnet 4.5","runtime":{"provider":"anthropic","model":"claude-sonnet-4-5","api":"anthropic-messages","base_url":"/proxy/anthropic"}},
				{"id":"gemini-2.5-flash-lite","name":"Gemini 2.5 Flash Lite","runtime":{"provider":"google-vertex","model":"gemini-2.5-flash-lite","api":"google-vertex","base_url":"/proxy/vertex"}},
				{"id":"google/gemini-3.1-pro-preview","name":"Gemini 3.1 Pro","runtime":{"provider":"google-vertex","model":"gemini-3.1-pro-preview","api":"google-vertex","base_url":"/proxy/vertex"}},
				{"id":"google/gemini-2.5-flash","name":"Gemini 2.5 Flash","runtime":{"provider":"google-vertex","model":"gemini-2.5-flash","api":"google-vertex","base_url":"/proxy/vertex"}},
				{"id":"x-ai/grok-4.20","name":"Grok 4.20","runtime":{"provider":"xai","model":"x-ai/grok-4.20","api":"openai-responses","base_url":"/proxy/xai/v1"}},
			{"id":"groq/qwen/qwen3-32b","name":"Qwen 3 32B","runtime":{"provider":"groq","model":"groq/qwen/qwen3-32b","api":"openai-responses","base_url":"/proxy/groq/v1"}},
			{"id":"openai/gpt-oss-120b","name":"GPT OSS 120B","runtime":{"provider":"a8c","model":"gpt-oss-120b","api":"openai-completions","base_url":"/proxy/a8c/v1"}},
			{"id":"anthropic/claude-sonnet-4.5","name":"Claude via OpenRouter","metadata":{"family":"claude","provider_logo_url":"/models/providers/anthropic.png"},"runtime":{"provider":"openrouter","model":"anthropic/claude-sonnet-4.5","api":"openai-completions","base_url":"/proxy/openrouter/v1","compat":{"supports_developer_role":false,"thinking_format":"openrouter","cache_control_format":"anthropic"}}}
		]}`))
	}))
	defer server.Close()

	client := &Client{
		Main: &Connector{
			AppServiceToken: "as-token",
		},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{UserMXID: "@test:beeper-staging.com"}},
	}
	models, err := client.aiServicesCatalogModels(context.Background(), aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ai.Model{}
	for _, model := range models {
		byID[model.ID] = model
	}
	if got := byID["claude-sonnet-4-5"]; got.API != ai.ApiAnthropicMessages || got.Provider != ai.ProviderAnthropic || got.BaseURL != server.URL+"/proxy/anthropic" {
		t.Fatalf("unexpected Anthropic route %#v", got)
	}
	if got := byID["gemini-2.5-flash-lite"]; got.API != ai.ApiGoogleVertex || got.Provider != ai.ProviderGoogleVertex || got.BaseURL != server.URL+"/proxy/vertex" {
		t.Fatalf("unexpected Vertex route %#v", got)
	}
	if got := byID["google/gemini-3.1-pro-preview"]; got.API != ai.ApiGoogleVertex || got.Provider != ai.ProviderGoogleVertex || got.BaseURL != server.URL+"/proxy/vertex" {
		t.Fatalf("unexpected Google route %#v", got)
	}
	if got := byID["google/gemini-2.5-flash"]; got.API != ai.ApiGoogleVertex || got.Provider != ai.ProviderGoogleVertex || got.BaseURL != server.URL+"/proxy/vertex" {
		t.Fatalf("unexpected legacy Google route %#v", got)
	}
	if got := byID["x-ai/grok-4.20"]; got.API != ai.ApiOpenAIResponses || got.Provider != ai.ProviderXAI || got.BaseURL != server.URL+"/proxy/xai/v1" {
		t.Fatalf("unexpected xAI route %#v", got)
	}
	if got := byID["groq/qwen/qwen3-32b"]; got.API != ai.ApiOpenAIResponses || got.Provider != ai.ProviderGroq || got.BaseURL != server.URL+"/proxy/groq/v1" {
		t.Fatalf("unexpected Groq route %#v", got)
	}
	if got := byID["openai/gpt-oss-120b"]; got.API != ai.ApiOpenAICompletions || got.Provider != ai.Provider("a8c") || got.BaseURL != server.URL+"/proxy/a8c/v1" {
		t.Fatalf("unexpected A8C route %#v", got)
	}
	if got := byID["anthropic/claude-sonnet-4.5"]; got.API != ai.ApiOpenAICompletions || got.Provider != ai.ProviderOpenRouter || got.BaseURL != server.URL+"/proxy/openrouter/v1" {
		t.Fatalf("unexpected OpenRouter route %#v", got)
	}
	if got := byID["anthropic/claude-sonnet-4.5"]; got.Compat["provider_logo_url"] != "/models/providers/anthropic.png" || got.Compat["runtime_model"] != "anthropic/claude-sonnet-4.5" || got.Compat["family"] != "claude" {
		t.Fatalf("expected AI Services catalog identity metadata, got %#v", got.Compat)
	}
	if got := byID["anthropic/claude-sonnet-4.5"]; got.Compat["supportsDeveloperRole"] != false || got.Compat["thinkingFormat"] != "openrouter" || got.Compat["cacheControlFormat"] != "anthropic" {
		t.Fatalf("expected AI Services catalog provider compat, got %#v", got.Compat)
	}
}

func TestResolveModelForProviderPreservesOpenAICatalogModelID(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		Models:   []ai.Model{{ID: "openai/gpt-5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses}},
	}
	model, ok := resolveModelForProvider(provider, string(aiid.ModelContactID(aiid.DefaultProvider, "openai/gpt-5.5")))
	if !ok || model.ID != "openai/gpt-5.5" {
		t.Fatalf("expected OpenAI catalog model to resolve, got ok=%v model=%#v", ok, model)
	}
	model, ok = resolveModelForProvider(provider, "openai/gpt-5.5")
	if !ok || model.ID != "openai/gpt-5.5" {
		t.Fatalf("expected OpenAI catalog model ID to resolve, got ok=%v model=%#v", ok, model)
	}
}

func TestMergeProviderCatalogModelsPreservesConfiguredCustomModels(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:       "custom",
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		BaseURL:  "https://custom.test/v1",
		Models: []ai.Model{
			{ID: "local-model", Name: "Local Model", Input: []string{"text", "image"}},
			{ID: "gpt-5.5"},
		},
	}
	catalog := []ai.Model{
		{
			ID:                   "gpt-5.5",
			Name:                 "GPT-5.5",
			Provider:             ai.ProviderOpenAI,
			API:                  ai.ApiOpenAIResponses,
			BaseURL:              "https://custom.test/v1",
			Reasoning:            true,
			DefaultThinkingLevel: ai.ModelThinkingLevelLow,
			ContextWindow:        128000,
		},
		{
			ID:       "catalog-only",
			Provider: ai.ProviderOpenAI,
			API:      ai.ApiOpenAIResponses,
		},
	}

	models := mergeProviderCatalogModels(provider, catalog)
	if len(models) != 2 {
		t.Fatalf("expected only configured models, got %#v", models)
	}
	if models[0].ID != "local-model" || models[0].Name != "Local Model" || !isImageModel(models[0]) {
		t.Fatalf("expected configured local model to be preserved, got %#v", models[0])
	}
	if models[1].ID != "gpt-5.5" || !models[1].Reasoning || models[1].ContextWindow != 128000 {
		t.Fatalf("expected matching configured model to inherit catalog metadata, got %#v", models[1])
	}
}

func TestResolveModelForProviderRejectsUnlistedCustomModelID(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:       "custom-openai",
		Provider: ai.ProviderOpenAI,
		API:      ai.ApiOpenAIResponses,
		Models:   []ai.Model{{ID: "gpt-5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses}},
	}
	if model, ok := resolveModelForProvider(provider, "custom-openai/openai/gpt-5.5"); ok {
		t.Fatalf("expected unlisted model ID to be rejected, got %#v", model)
	}
	if model, ok := resolveModelForProvider(provider, "whateveristyped"); ok {
		t.Fatalf("expected arbitrary model ID to be rejected, got %#v", model)
	}
}

func TestResolveModelForProviderRejectsArbitraryDefaultProviderModelID(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:       aiid.DefaultProvider,
		Provider: ai.ProviderOpenRouter,
		API:      ai.ApiOpenAICompletions,
		Models:   []ai.Model{{ID: "gpt-5", Provider: ai.ProviderOpenRouter, API: ai.ApiOpenAICompletions}},
	}
	if model, ok := resolveModelForProvider(provider, "beeper/openai/gpt-5"); ok {
		t.Fatalf("expected arbitrary default provider model ID to be rejected, got %#v", model)
	}
}

func TestNewAIChatPortalKeyCreatesFreshChatPortal(t *testing.T) {
	first := newAIChatPortalKey("login")
	second := newAIChatPortalKey("login")
	if first.Receiver != "login" || second.Receiver != "login" {
		t.Fatalf("unexpected receiver: %#v %#v", first, second)
	}
	if first.ID == second.ID {
		t.Fatalf("expected fresh portal IDs, got %q twice", first.ID)
	}
	if !strings.HasPrefix(string(first.ID), "chat:") || !strings.HasPrefix(string(second.ID), "chat:") {
		t.Fatalf("expected chat portal IDs, got %#v %#v", first, second)
	}
}

func TestAIChatMembersUseGlobalAssistantGhostWithoutNetworkBotFlag(t *testing.T) {
	members := aiChatMembers()
	if members == nil || !members.IsFull || members.OtherUserID != aiid.AssistantUserID() {
		t.Fatalf("unexpected AI chat members %#v", members)
	}
	if !members.ExcludeChangesFromTimeline {
		t.Fatalf("expected synthetic AI member changes to be excluded from timeline")
	}
	if member, ok := members.MemberMap[networkid.UserID("")]; !ok || !member.IsFromMe {
		t.Fatalf("expected Matrix user member, got %#v", members.MemberMap)
	} else if member.MemberEventExtra["com.beeper.exclude_from_timeline"] != true {
		t.Fatalf("expected Matrix user member event to be excluded from timeline, got %#v", member.MemberEventExtra)
	}
	if member, ok := members.MemberMap[aiid.AssistantUserID()]; !ok || member.Sender != aiid.AssistantUserID() {
		t.Fatalf("expected assistant ghost member, got %#v", members.MemberMap)
	} else if member.UserInfo == nil || member.UserInfo.Avatar == nil || string(member.UserInfo.Avatar.MXC) != defaultAIAssistantAvatarMXC {
		t.Fatalf("expected assistant ghost avatar %q, got %#v", defaultAIAssistantAvatarMXC, member.UserInfo)
	} else if member.UserInfo.IsBot == nil || *member.UserInfo.IsBot {
		t.Fatalf("expected assistant ghost to not be marked as a network bot, got %#v", member.UserInfo)
	} else if member.MemberEventExtra["com.beeper.exclude_from_timeline"] != true {
		t.Fatalf("expected assistant ghost member event to be excluded from timeline, got %#v", member.MemberEventExtra)
	}
	if members.PowerLevels == nil {
		t.Fatalf("expected AI chat power levels")
	}
	for _, evtType := range []event.Type{
		event.StateRoomName,
		event.StateTopic,
		event.StateBeeperDisappearingTimer,
	} {
		if level, ok := members.PowerLevels.Events[evtType]; !ok || level != 0 {
			t.Fatalf("expected %s power level 0, got %d (present=%v)", evtType.Type, level, ok)
		}
	}
}

func TestModelRoomDescriptionUsesDisplayName(t *testing.T) {
	provider := aiid.ProviderConfig{ID: "local", DisplayName: "Local"}
	model := ai.Model{ID: "small", Name: "Small Chat"}
	if got := modelRoomDescription(provider, model); got != "AI Chat with Small Chat" {
		t.Fatalf("unexpected model room description %q", got)
	}
}

func TestModelWelcomeNoticeUsesDisplayName(t *testing.T) {
	provider := aiid.ProviderConfig{ID: "beeper"}
	model := ai.Model{ID: "anthropic/claude-opus-4.5", Name: "Claude Opus 4.5"}
	if got := modelWelcomeNoticeText(provider, model); got != "You are chatting with Claude Opus 4.5. AI can make mistakes." {
		t.Fatalf("unexpected model welcome notice %q", got)
	}
}

func TestSearchUsersFiltersModelContacts(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:     "local",
		Models: []ai.Model{{ID: "small"}, {ID: "large"}},
	}
	client := &Client{Main: &Connector{}, UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}}
	results, err := client.SearchUsers(context.Background(), "large")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
}

func TestSearchUsersRejectsArbitraryModelContact(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:          "local",
		DisplayName: "Local",
		Provider:    "local",
		API:         ai.ApiOpenAIResponses,
		Models:      []ai.Model{{ID: "listed"}},
	}
	client := &Client{Main: &Connector{}, UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}}
	results, err := client.SearchUsers(context.Background(), "whateveristyped")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no arbitrary model contact, got %#v", results)
	}
}

func TestContactListIncludesLoginProvider(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:           "openrouter",
		DisplayName:  "OpenRouter",
		Provider:     ai.ProviderOpenRouter,
		API:          ai.ApiOpenAICompletions,
		BaseURL:      "https://openrouter.ai/api/v1",
		DefaultModel: "anthropic/claude-sonnet-4.5",
		Models:       []ai.Model{{ID: "anthropic/claude-sonnet-4.5"}},
	}
	client := &Client{
		Main: conn,
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			ID:       "login",
			Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
		}},
	}

	contacts, err := client.GetContactList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, contact := range contacts {
		for _, identifier := range contact.UserInfo.Identifiers {
			got[identifier] = true
		}
	}
	for _, want := range []string{
		"openrouter/anthropic/claude-sonnet-4.5",
	} {
		if !got[want] {
			t.Fatalf("expected contact %s in %#v", want, got)
		}
	}
	if got["disabled/gpt-5"] {
		t.Fatalf("unexpected provider leaked into contact list: %#v", got)
	}
}
