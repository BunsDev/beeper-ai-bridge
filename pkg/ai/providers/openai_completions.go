package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
)

type OpenAICompletionsOptions struct {
	ai.StreamOptions
	ToolChoice      any
	ReasoningEffort *ai.ThinkingLevel
}

func StreamSimpleOpenAICompletions(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	if options.APIKey == "" {
		options.APIKey = getEnvAPIKey(model.Provider)
	}
	if options.APIKey == "" {
		stream := ai.NewAssistantMessageEventStream()
		go pushError(stream, model, "No API key for provider: "+string(model.Provider))
		return stream
	}
	return StreamOpenAICompletions(ctx, model, llmContext, OpenAICompletionsOptions{
		StreamOptions:   options.StreamOptions,
		ReasoningEffort: simpleReasoningEffort(model, options.Reasoning),
	})
}

func StreamOpenAICompletions(ctx context.Context, model ai.Model, llmContext ai.Context, options OpenAICompletionsOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		output := newAssistant(model)
		params := BuildCompletionsParams(model, llmContext, options)
		if options.OnPayload != nil {
			if next, ok, err := options.OnPayload(params, model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			} else if ok {
				nextParams, ok := next.(map[string]any)
				if !ok {
					pushFinalError(stream, &output, "onPayload returned unsupported OpenAI request body")
					return
				}
				params = nextParams
			}
		}

		var rawResponse *http.Response
		client, requestOptions, err := newClient(model, llmContext, options.StreamOptions)
		if err != nil {
			pushFinalError(stream, &output, err.Error())
			return
		}
		requestOptions = append(requestOptions, option.WithResponseInto(&rawResponse))
		sdkStream := client.Chat.Completions.NewStreaming(ctx, param.Override[openaisdk.ChatCompletionNewParams](params), requestOptions...)
		defer sdkStream.Close()

		if options.OnResponse != nil && rawResponse != nil {
			if err := options.OnResponse(providerResponse(rawResponse), model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			}
		}
		stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
		state := newCompletionsStreamState()
		for sdkStream.Next() {
			chunk := sdkStream.Current()
			var raw map[string]any
			if err := json.Unmarshal([]byte(chunk.RawJSON()), &raw); err != nil {
				continue
			}
			state.apply(stream, &output, model, raw)
		}
		if err := sdkStream.Err(); err != nil {
			pushFinalError(stream, &output, formatOpenAIError(err))
			return
		}
		if !state.hasFinishReason {
			pushFinalError(stream, &output, "Stream ended without finish_reason")
			return
		}
		finishBlocks(stream, &output, state.blocks)
		stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
	}()
	return stream
}

func BuildCompletionsParams(model ai.Model, llmContext ai.Context, options OpenAICompletionsOptions) map[string]any {
	compat := ResolveOpenAICompletionsCompat(model)
	cacheRetention := resolveCacheRetention(options.CacheRetention)
	params := map[string]any{
		"model":    model.ID,
		"messages": ConvertCompletionsMessagesWithCompat(model, llmContext, compat),
		"stream":   true,
	}
	if compat.SupportsUsageInStreaming {
		params["stream_options"] = map[string]any{"include_usage": true}
	}
	if compat.SupportsStore {
		params["store"] = false
	}
	if options.MaxTokens != nil {
		if compat.MaxTokensField == "max_tokens" {
			params["max_tokens"] = *options.MaxTokens
		} else {
			params["max_completion_tokens"] = *options.MaxTokens
		}
	}
	if options.Temperature != nil {
		params["temperature"] = *options.Temperature
	}
	if len(llmContext.Tools) > 0 {
		params["tools"] = ConvertCompletionsToolsWithCompat(llmContext.Tools, compat)
		if compat.ZaiToolStream {
			params["tool_stream"] = true
		}
	} else if hasToolHistory(llmContext.Messages) {
		params["tools"] = []map[string]any{}
	}
	if cacheControl := compatCacheControl(compat, cacheRetention); cacheControl != nil {
		applyAnthropicCacheControl(params["messages"], params["tools"], cacheControl)
	}
	if options.ToolChoice != nil {
		params["tool_choice"] = options.ToolChoice
	}
	if key := promptCacheKey(model, options.SessionID, cacheRetention, compat.SupportsLongCacheRetention); key != "" {
		params["prompt_cache_key"] = key
	}
	if cacheRetention == ai.CacheRetentionLong && compat.SupportsLongCacheRetention {
		params["prompt_cache_retention"] = "24h"
	}
	applyCompletionsThinkingParams(params, model, options.ReasoningEffort, compat)
	if (model.Provider == "openrouter" || strings.Contains(model.BaseURL, "openrouter.ai")) && len(compat.OpenRouterRouting) > 0 {
		params["provider"] = compat.OpenRouterRouting
	}
	if strings.Contains(model.BaseURL, "ai-gateway.vercel.sh") && len(compat.VercelGatewayRouting) > 0 {
		params["providerOptions"] = map[string]any{"gateway": compat.VercelGatewayRouting}
	}
	return params
}

func ConvertMessages(model ai.Model, llmContext ai.Context) []map[string]any {
	return ConvertCompletionsMessages(model, llmContext)
}

func ConvertCompletionsMessages(model ai.Model, llmContext ai.Context) []map[string]any {
	return ConvertCompletionsMessagesWithCompat(model, llmContext, ResolveOpenAICompletionsCompat(model))
}

func ConvertCompletionsMessagesWithCompat(model ai.Model, llmContext ai.Context, compat ResolvedOpenAICompletionsCompat) []map[string]any {
	messages := []map[string]any{}
	if llmContext.SystemPrompt != "" {
		role := "system"
		if model.Reasoning && compat.SupportsDeveloperRole {
			role = "developer"
		}
		messages = append(messages, map[string]any{"role": role, "content": aiutils.SanitizeSurrogates(llmContext.SystemPrompt)})
	}
	transformedMessages := transformMessages(llmContext.Messages, model, normalizeCompletionsToolCallID)
	lastRole := ""
	for i := 0; i < len(transformedMessages); i++ {
		msg := transformedMessages[i]
		if compat.RequiresAssistantAfterToolResult && lastRole == "toolResult" && msg.Role == "user" {
			messages = append(messages, map[string]any{"role": "assistant", "content": "I have processed the tool results."})
		}
		switch msg.Role {
		case "user":
			content := completionsUserContent(msg.Content)
			if isEmptyContent(content) {
				continue
			}
			messages = append(messages, map[string]any{"role": "user", "content": content})
		case "assistant":
			assistant := assistantMessageParamWithCompat(msg, model, compat)
			if assistantHasContent(assistant) {
				messages = append(messages, assistant)
			}
		case "toolResult":
			imageBlocks := []map[string]any{}
			for ; i < len(transformedMessages) && transformedMessages[i].Role == "toolResult"; i++ {
				toolMsg := transformedMessages[i]
				text := toolResultText(toolMsg.Content)
				if text == "" {
					text = "(see attached image)"
				}
				toolResult := map[string]any{"role": "tool", "tool_call_id": toolMsg.ToolCallID, "content": text}
				if compat.RequiresToolResultName && toolMsg.ToolName != "" {
					toolResult["name"] = toolMsg.ToolName
				}
				messages = append(messages, toolResult)
				if modelSupportsImage(model) {
					for _, block := range contentBlocks(toolMsg.Content) {
						if block.Type == "image" {
							imageBlocks = append(imageBlocks, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + block.MimeType + ";base64," + block.Data}})
						}
					}
				}
			}
			i--
			if len(imageBlocks) > 0 {
				if compat.RequiresAssistantAfterToolResult {
					messages = append(messages, map[string]any{"role": "assistant", "content": "I have processed the tool results."})
				}
				content := append([]map[string]any{{"type": "text", "text": "Attached image(s) from tool result:"}}, imageBlocks...)
				messages = append(messages, map[string]any{"role": "user", "content": content})
				lastRole = "user"
			} else {
				lastRole = "toolResult"
			}
			continue
		}
		lastRole = msg.Role
	}
	return messages
}

func ConvertCompletionsTools(tools []ai.Tool) []map[string]any {
	return ConvertCompletionsToolsWithCompat(tools, ResolveOpenAICompletionsCompat(ai.Model{}))
}

func ConvertCompletionsToolsWithCompat(tools []ai.Tool, compat ResolvedOpenAICompletionsCompat) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		fn := map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}
		if compat.SupportsStrictMode {
			fn["strict"] = false
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

func compatCacheControl(compat ResolvedOpenAICompletionsCompat, retention ai.CacheRetention) map[string]any {
	if compat.CacheControlFormat != "anthropic" || retention == ai.CacheRetentionNone {
		return nil
	}
	cacheControl := map[string]any{"type": "ephemeral"}
	if retention == ai.CacheRetentionLong && compat.SupportsLongCacheRetention {
		cacheControl["ttl"] = "1h"
	}
	return cacheControl
}

func applyAnthropicCacheControl(messagesValue any, toolsValue any, cacheControl map[string]any) {
	messages, _ := messagesValue.([]map[string]any)
	tools, _ := toolsValue.([]map[string]any)
	addCacheControlToSystemPrompt(messages, cacheControl)
	addCacheControlToLastTool(tools, cacheControl)
	addCacheControlToLastConversationMessage(messages, cacheControl)
}

func addCacheControlToSystemPrompt(messages []map[string]any, cacheControl map[string]any) {
	for _, message := range messages {
		if message["role"] == "system" || message["role"] == "developer" {
			addCacheControlToTextContent(message, cacheControl)
			return
		}
	}
}

func addCacheControlToLastConversationMessage(messages []map[string]any, cacheControl map[string]any) {
	for i := len(messages) - 1; i >= 0; i-- {
		role := messages[i]["role"]
		if role == "user" || role == "assistant" {
			if addCacheControlToTextContent(messages[i], cacheControl) {
				return
			}
		}
	}
}

func addCacheControlToLastTool(tools []map[string]any, cacheControl map[string]any) {
	if len(tools) == 0 {
		return
	}
	tools[len(tools)-1]["cache_control"] = cacheControl
}

func addCacheControlToTextContent(message map[string]any, cacheControl map[string]any) bool {
	switch content := message["content"].(type) {
	case string:
		if content == "" {
			return false
		}
		message["content"] = []map[string]any{{"type": "text", "text": content, "cache_control": cacheControl}}
		return true
	case []map[string]any:
		for i := len(content) - 1; i >= 0; i-- {
			if content[i]["type"] == "text" {
				content[i]["cache_control"] = cacheControl
				return true
			}
		}
	case []any:
		for i := len(content) - 1; i >= 0; i-- {
			part, ok := content[i].(map[string]any)
			if ok && part["type"] == "text" {
				part["cache_control"] = cacheControl
				return true
			}
		}
	}
	return false
}

type openAIClientConfig struct {
	APIKey  string
	BaseURL string
	Headers map[string]string
}

func newClient(model ai.Model, llmContext ai.Context, options ai.StreamOptions) (openaisdk.Client, []option.RequestOption, error) {
	config, err := buildOpenAIClientConfig(model, llmContext, options)
	if err != nil {
		return openaisdk.Client{}, nil, err
	}
	requestOptions := []option.RequestOption{option.WithAPIKey(config.APIKey)}
	if config.BaseURL != "" {
		requestOptions = append(requestOptions, option.WithBaseURL(config.BaseURL))
	}
	for key, value := range config.Headers {
		requestOptions = append(requestOptions, option.WithHeader(key, value))
	}
	if options.TimeoutMs != nil {
		requestOptions = append(requestOptions, option.WithRequestTimeout(time.Duration(*options.TimeoutMs)*time.Millisecond))
	}
	if options.MaxRetries != nil {
		requestOptions = append(requestOptions, option.WithMaxRetries(*options.MaxRetries))
	}
	return openaisdk.NewClient(requestOptions...), requestOptions, nil
}

func buildOpenAIClientConfig(model ai.Model, llmContext ai.Context, options ai.StreamOptions) (openAIClientConfig, error) {
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = getEnvAPIKey(model.Provider)
	}
	baseURL := model.BaseURL
	if isCloudflareProvider(model.Provider) {
		resolved, err := ResolveCloudflareBaseURL(model)
		if err != nil {
			return openAIClientConfig{}, err
		}
		baseURL = resolved
	}
	headers := map[string]string{}
	for key, value := range model.Headers {
		headers[key] = value
	}
	if model.Provider == ai.ProviderGitHubCopilot {
		for key, value := range buildCopilotDynamicHeaders(llmContext.Messages) {
			headers[key] = value
		}
	}
	if options.SessionID != "" && resolveCacheRetention(options.CacheRetention) != ai.CacheRetentionNone {
		headers["x-client-request-id"] = options.SessionID
		if model.API == ai.ApiOpenAIResponses || ResolveOpenAICompletionsCompat(model).SendSessionAffinityHeaders {
			headers["session_id"] = options.SessionID
		}
		if model.API == ai.ApiOpenAICompletions && ResolveOpenAICompletionsCompat(model).SendSessionAffinityHeaders {
			headers["x-session-affinity"] = options.SessionID
		}
	}
	for key, value := range options.Headers {
		headers[key] = value
	}
	if model.Provider == ai.ProviderCloudflareAIGateway {
		if _, ok := headerValue(headers, nil, "authorization"); !ok {
			headers["Authorization"] = ""
		}
		headers["cf-aig-authorization"] = "Bearer " + apiKey
	}
	return openAIClientConfig{APIKey: apiKey, BaseURL: baseURL, Headers: headers}, nil
}

func isCloudflareProvider(provider ai.Provider) bool {
	return provider == ai.ProviderCloudflareWorkersAI || provider == ai.ProviderCloudflareAIGateway
}

var cloudflarePlaceholderPattern = regexp.MustCompile(`\{([A-Z_][A-Z0-9_]*)\}`)

func ResolveCloudflareBaseURL(model ai.Model) (string, error) {
	if !strings.Contains(model.BaseURL, "{") {
		return model.BaseURL, nil
	}
	var missing string
	baseURL := cloudflarePlaceholderPattern.ReplaceAllStringFunc(model.BaseURL, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
		value := os.Getenv(name)
		if value == "" && missing == "" {
			missing = name
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("%s is required for provider %s but is not set.", missing, model.Provider)
	}
	return baseURL, nil
}

func headerValue(primary map[string]string, secondary map[string]string, want string) (string, bool) {
	for key, value := range primary {
		if strings.EqualFold(key, want) {
			return value, true
		}
	}
	for key, value := range secondary {
		if strings.EqualFold(key, want) {
			return value, true
		}
	}
	return "", false
}

func getEnvAPIKey(provider ai.Provider) string {
	return ai.GetEnvAPIKey(provider)
}

func newAssistant(model ai.Model) ai.Message {
	return ai.Message{Role: "assistant", Content: []ai.ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), StopReason: ai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
}

func pushError(stream *ai.AssistantMessageEventStream, model ai.Model, message string) {
	output := newAssistant(model)
	pushFinalError(stream, &output, message)
}

func pushFinalError(stream *ai.AssistantMessageEventStream, output *ai.Message, message string) {
	output.StopReason = ai.StopReasonError
	output.ErrorMessage = message
	stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: output.StopReason, Error: output})
}

func providerResponse(response *http.Response) ai.ProviderResponse {
	return ai.ProviderResponse{Status: response.StatusCode, Headers: aiutils.HeadersToRecord(response.Header)}
}

func formatOpenAIError(err error) string {
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		message := apiErr.Message
		if message == "" {
			message = apiErr.Error()
		}
		return "OpenAI API error (" + strconv.Itoa(apiErr.StatusCode) + "): " + message
	}
	return err.Error()
}

func parseJSONMap(raw string) map[string]any {
	return aiutils.ParseStreamingJSON(raw)
}
