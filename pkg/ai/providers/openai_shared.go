package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
)

var invalidToolCallIDChar = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func contentBlocks(content any) []ai.ContentBlock {
	switch value := content.(type) {
	case []ai.ContentBlock:
		return value
	case []any:
		blocks := make([]ai.ContentBlock, 0, len(value))
		for _, item := range value {
			raw, _ := json.Marshal(item)
			var block ai.ContentBlock
			if json.Unmarshal(raw, &block) == nil {
				blocks = append(blocks, block)
			}
		}
		return blocks
	default:
		raw, _ := json.Marshal(value)
		var blocks []ai.ContentBlock
		_ = json.Unmarshal(raw, &blocks)
		return blocks
	}
}

func inferCopilotInitiator(messages []ai.Message) string {
	if len(messages) == 0 || messages[len(messages)-1].Role == "user" {
		return "user"
	}
	return "agent"
}

func hasCopilotVisionInput(messages []ai.Message) bool {
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "toolResult" {
			continue
		}
		for _, block := range contentBlocks(msg.Content) {
			if block.Type == "image" {
				return true
			}
		}
	}
	return false
}

func buildCopilotDynamicHeaders(messages []ai.Message) map[string]string {
	headers := map[string]string{
		"X-Initiator":   inferCopilotInitiator(messages),
		"Openai-Intent": "conversation-edits",
	}
	if hasCopilotVisionInput(messages) {
		headers["Copilot-Vision-Request"] = "true"
	}
	return headers
}

type ResolvedOpenAICompletionsCompat struct {
	SupportsStore                               bool
	SupportsDeveloperRole                       bool
	SupportsReasoningEffort                     bool
	SupportsUsageInStreaming                    bool
	MaxTokensField                              string
	RequiresToolResultName                      bool
	RequiresAssistantAfterToolResult            bool
	RequiresThinkingAsText                      bool
	RequiresReasoningContentOnAssistantMessages bool
	ThinkingFormat                              string
	OpenRouterRouting                           map[string]any
	VercelGatewayRouting                        map[string]any
	ZaiToolStream                               bool
	SupportsStrictMode                          bool
	CacheControlFormat                          string
	SendSessionAffinityHeaders                  bool
	SupportsLongCacheRetention                  bool
}

type ResolvedOpenAIResponsesCompat struct {
	SendSessionIDHeader        bool
	SupportsLongCacheRetention bool
}

func ResolveOpenAIResponsesCompat(model ai.Model) ResolvedOpenAIResponsesCompat {
	return ResolvedOpenAIResponsesCompat{
		SendSessionIDHeader:        compatBool(model, "sendSessionIdHeader", true),
		SupportsLongCacheRetention: compatBool(model, "supportsLongCacheRetention", true),
	}
}

func ResolveOpenAICompletionsCompat(model ai.Model) ResolvedOpenAICompletionsCompat {
	detected := detectOpenAICompletionsCompat(model)
	if model.Compat == nil {
		return detected
	}
	detected.SupportsStore = compatBool(model, "supportsStore", detected.SupportsStore)
	detected.SupportsDeveloperRole = compatBool(model, "supportsDeveloperRole", detected.SupportsDeveloperRole)
	detected.SupportsReasoningEffort = compatBool(model, "supportsReasoningEffort", detected.SupportsReasoningEffort)
	detected.SupportsUsageInStreaming = compatBool(model, "supportsUsageInStreaming", detected.SupportsUsageInStreaming)
	detected.MaxTokensField = compatString(model, "maxTokensField", detected.MaxTokensField)
	detected.RequiresToolResultName = compatBool(model, "requiresToolResultName", detected.RequiresToolResultName)
	detected.RequiresAssistantAfterToolResult = compatBool(model, "requiresAssistantAfterToolResult", detected.RequiresAssistantAfterToolResult)
	detected.RequiresThinkingAsText = compatBool(model, "requiresThinkingAsText", detected.RequiresThinkingAsText)
	detected.RequiresReasoningContentOnAssistantMessages = compatBool(model, "requiresReasoningContentOnAssistantMessages", detected.RequiresReasoningContentOnAssistantMessages)
	detected.ThinkingFormat = compatString(model, "thinkingFormat", detected.ThinkingFormat)
	detected.OpenRouterRouting = compatMap(model, "openRouterRouting", map[string]any{})
	detected.VercelGatewayRouting = compatMap(model, "vercelGatewayRouting", detected.VercelGatewayRouting)
	detected.ZaiToolStream = compatBool(model, "zaiToolStream", detected.ZaiToolStream)
	detected.SupportsStrictMode = compatBool(model, "supportsStrictMode", detected.SupportsStrictMode)
	detected.CacheControlFormat = compatString(model, "cacheControlFormat", detected.CacheControlFormat)
	detected.SendSessionAffinityHeaders = compatBool(model, "sendSessionAffinityHeaders", detected.SendSessionAffinityHeaders)
	detected.SupportsLongCacheRetention = compatBool(model, "supportsLongCacheRetention", detected.SupportsLongCacheRetention)
	return detected
}

func detectOpenAICompletionsCompat(model ai.Model) ResolvedOpenAICompletionsCompat {
	provider := string(model.Provider)
	baseURL := model.BaseURL
	isZai := provider == "zai" || strings.Contains(baseURL, "api.z.ai")
	isTogether := provider == "together" || strings.Contains(baseURL, "api.together.ai") || strings.Contains(baseURL, "api.together.xyz")
	isMoonshot := provider == "moonshotai" || provider == "moonshotai-cn" || strings.Contains(baseURL, "api.moonshot.")
	isCloudflareWorkersAI := provider == "cloudflare-workers-ai" || strings.Contains(baseURL, "api.cloudflare.com")
	isCloudflareAIGateway := provider == "cloudflare-ai-gateway" || strings.Contains(baseURL, "gateway.ai.cloudflare.com")
	isA8C := provider == "a8c" || strings.Contains(baseURL, "/proxy/a8c/")
	isNonStandard := provider == "cerebras" ||
		strings.Contains(baseURL, "cerebras.ai") ||
		provider == "xai" ||
		strings.Contains(baseURL, "api.x.ai") ||
		isTogether ||
		strings.Contains(baseURL, "chutes.ai") ||
		strings.Contains(baseURL, "deepseek.com") ||
		isZai ||
		isMoonshot ||
		provider == "opencode" ||
		strings.Contains(baseURL, "opencode.ai") ||
		isCloudflareWorkersAI ||
		isCloudflareAIGateway
	useMaxTokens := strings.Contains(baseURL, "chutes.ai") || isMoonshot || isCloudflareAIGateway || isTogether
	isGrok := provider == "xai" || strings.Contains(baseURL, "api.x.ai")
	isDeepSeek := provider == "deepseek" || strings.Contains(baseURL, "deepseek.com")
	thinkingFormat := "openai"
	if isDeepSeek {
		thinkingFormat = "deepseek"
	} else if isZai {
		thinkingFormat = "zai"
	} else if isTogether {
		thinkingFormat = "together"
	} else if isA8C {
		thinkingFormat = "a8c"
	} else if provider == "openrouter" || strings.Contains(baseURL, "openrouter.ai") {
		thinkingFormat = "openrouter"
	}
	maxTokensField := "max_completion_tokens"
	if useMaxTokens {
		maxTokensField = "max_tokens"
	}
	cacheControlFormat := ""
	if provider == "openrouter" && strings.HasPrefix(model.ID, "anthropic/") {
		cacheControlFormat = "anthropic"
	}
	return ResolvedOpenAICompletionsCompat{
		SupportsStore:                               !isNonStandard,
		SupportsDeveloperRole:                       !isNonStandard,
		SupportsReasoningEffort:                     !isGrok && !isZai && !isMoonshot && !isTogether && !isCloudflareAIGateway,
		SupportsUsageInStreaming:                    true,
		MaxTokensField:                              maxTokensField,
		RequiresToolResultName:                      false,
		RequiresAssistantAfterToolResult:            false,
		RequiresThinkingAsText:                      false,
		RequiresReasoningContentOnAssistantMessages: isDeepSeek,
		ThinkingFormat:                              thinkingFormat,
		OpenRouterRouting:                           map[string]any{},
		VercelGatewayRouting:                        map[string]any{},
		ZaiToolStream:                               false,
		SupportsStrictMode:                          !isMoonshot && !isTogether && !isCloudflareAIGateway,
		CacheControlFormat:                          cacheControlFormat,
		SendSessionAffinityHeaders:                  false,
		SupportsLongCacheRetention:                  !(isTogether || isCloudflareWorkersAI || isCloudflareAIGateway),
	}
}

func compatBool(model ai.Model, key string, fallback bool) bool {
	value, ok := model.Compat[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func compatString(model ai.Model, key string, fallback string) string {
	value, ok := model.Compat[key].(string)
	if !ok || value == "" {
		return fallback
	}
	return value
}

func compatMap(model ai.Model, key string, fallback map[string]any) map[string]any {
	value, ok := model.Compat[key].(map[string]any)
	if !ok {
		return fallback
	}
	return value
}

func assistantMessageParamWithCompat(msg ai.Message, model ai.Model, compat ResolvedOpenAICompletionsCompat) map[string]any {
	out := map[string]any{"role": "assistant", "content": nil}
	if compat.RequiresAssistantAfterToolResult {
		out["content"] = ""
	}
	text := ""
	thinkingBlocks := []ai.ContentBlock{}
	toolCalls := []map[string]any{}
	for _, block := range contentBlocks(msg.Content) {
		if block.Type == "text" {
			if strings.TrimSpace(block.Text) != "" {
				text += aiutils.SanitizeSurrogates(block.Text)
			}
		}
		if block.Type == "thinking" && strings.TrimSpace(block.Thinking) != "" {
			thinkingBlocks = append(thinkingBlocks, block)
		}
		if block.Type == "toolCall" {
			toolCalls = append(toolCalls, map[string]any{
				"id": block.ID, "type": "function",
				"function": map[string]any{"name": block.Name, "arguments": mustJSON(block.Arguments)},
			})
		}
	}
	if len(thinkingBlocks) > 0 {
		thinkingText := make([]string, 0, len(thinkingBlocks))
		for _, block := range thinkingBlocks {
			thinkingText = append(thinkingText, aiutils.SanitizeSurrogates(block.Thinking))
		}
		if compat.RequiresThinkingAsText {
			parts := []map[string]any{{"type": "text", "text": strings.Join(thinkingText, "\n\n")}}
			if text != "" {
				parts = append(parts, map[string]any{"type": "text", "text": text})
				text = ""
			}
			out["content"] = parts
		} else {
			signature := thinkingBlocks[0].ThinkingSignature
			if msg.Provider == "opencode-go" && signature == "reasoning" {
				signature = "reasoning_content"
			}
			if signature != "" {
				out[signature] = strings.Join(thinkingText, "\n")
			}
		}
	}
	if text != "" {
		out["content"] = text
	}
	if len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	if compat.RequiresReasoningContentOnAssistantMessages && model.Reasoning {
		if _, ok := out["reasoning_content"]; !ok {
			out["reasoning_content"] = ""
		}
	}
	return out
}

func completionsUserContent(content any) any {
	if text, ok := content.(string); ok {
		return aiutils.SanitizeSurrogates(text)
	}
	parts := []map[string]any{}
	for _, block := range contentBlocks(content) {
		switch block.Type {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": aiutils.SanitizeSurrogates(block.Text)})
		case "image":
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + block.MimeType + ";base64," + block.Data}})
		case "audio":
			part, _ := inputAudioPart(block)
			parts = append(parts, part)
		}
	}
	return parts
}

func isEmptyContent(content any) bool {
	switch value := content.(type) {
	case string:
		return value == ""
	case []map[string]any:
		return len(value) == 0
	default:
		return content == nil
	}
}

func assistantHasContent(assistant map[string]any) bool {
	if toolCalls, ok := assistant["tool_calls"].([]map[string]any); ok && len(toolCalls) > 0 {
		return true
	}
	content := assistant["content"]
	if content == nil {
		return false
	}
	if text, ok := content.(string); ok {
		return text != ""
	}
	if parts, ok := content.([]map[string]any); ok {
		return len(parts) > 0
	}
	return true
}

func modelSupportsImage(model ai.Model) bool {
	for _, input := range model.Input {
		if input == "image" {
			return true
		}
	}
	return false
}

func modelSupportsAudio(model ai.Model) bool {
	for _, input := range model.Input {
		if input == "audio" {
			return true
		}
	}
	return false
}

func inputAudioPart(block ai.ContentBlock) (map[string]any, bool) {
	format, ok := inputAudioFormat(block.MimeType)
	if !ok {
		return map[string]any{"type": "text", "text": "(audio omitted: unsupported audio format " + block.MimeType + ")"}, false
	}
	return map[string]any{
		"type": "input_audio",
		"input_audio": map[string]any{
			"data":   block.Data,
			"format": format,
		},
	}, true
}

func inputAudioFormat(mimeType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "audio/wav", "audio/x-wav":
		return "wav", true
	case "audio/mpeg", "audio/mp3":
		return "mp3", true
	default:
		return "", false
	}
}

func normalizeCompletionsToolCallID(id string, model ai.Model, _ ai.Message) string {
	if strings.Contains(id, "|") {
		callID := strings.Split(id, "|")[0]
		return truncateString(invalidToolCallIDChar.ReplaceAllString(callID, "_"), 40)
	}
	if model.Provider == "openai" {
		return truncateString(id, 40)
	}
	return id
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func shortHash(value string) string {
	return aiutils.ShortHash(value)
}

func toolResultText(content any) string {
	parts := []string{}
	for _, block := range contentBlocks(content) {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, aiutils.SanitizeSurrogates(block.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func finishBlocks(stream *ai.AssistantMessageEventStream, output *ai.Message, blocks []ai.ContentBlock) {
	output.Content = blocks
	for i, block := range blocks {
		switch block.Type {
		case "text":
			stream.Push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: i, Content: block.Text, Partial: output})
		case "thinking":
			stream.Push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: i, Content: block.Thinking, Partial: output})
		case "toolCall":
			stream.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: i, ToolCall: &ai.ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: block.Arguments}, Partial: output})
		}
	}
}

type completionsStreamState struct {
	blocks             []ai.ContentBlock
	textIndex          *int
	thinkingIndex      *int
	toolContentByIndex map[int]int
	toolContentByID    map[string]int
	toolArgsByContent  map[int]string
	hasFinishReason    bool
}

func newCompletionsStreamState() *completionsStreamState {
	return &completionsStreamState{
		toolContentByIndex: map[int]int{},
		toolContentByID:    map[string]int{},
		toolArgsByContent:  map[int]string{},
	}
}

func (s *completionsStreamState) apply(stream *ai.AssistantMessageEventStream, output *ai.Message, model ai.Model, chunk map[string]any) {
	if output.ResponseID == "" {
		if id, ok := chunk["id"].(string); ok {
			output.ResponseID = id
		}
	}
	if responseModel, ok := chunk["model"].(string); ok && responseModel != "" && responseModel != model.ID {
		output.ResponseModel = responseModel
	}
	if usage, ok := chunk["usage"].(map[string]any); ok {
		output.Usage = parseCompletionsUsageMap(usage, model)
	}
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return
	}
	choice, _ := choices[0].(map[string]any)
	if _, ok := chunk["usage"]; !ok {
		if usage, ok := choice["usage"].(map[string]any); ok {
			output.Usage = parseCompletionsUsageMap(usage, model)
		}
	}
	if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
		output.StopReason = mapStopReason(reason)
		if output.StopReason == ai.StopReasonError {
			output.ErrorMessage = "Provider finish_reason: " + reason
		}
		s.hasFinishReason = true
	}
	delta, _ := choice["delta"].(map[string]any)
	if text, ok := delta["content"].(string); ok && text != "" {
		index := s.ensureText(stream, output)
		s.blocks[index].Text += text
		output.Content = s.blocks
		stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: text, Partial: output})
	}
	if annotations, ok := delta["annotations"].([]any); ok {
		index := s.ensureText(stream, output)
		output.Citations = append(output.Citations, providerCitationsFromAny(annotations, model.Provider, index)...)
		stream.Push(ai.AssistantMessageEvent{Type: "source", Partial: output})
	}
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		if reasoning, ok := delta[field].(string); ok && reasoning != "" {
			signature := field
			if model.Provider == "opencode-go" && field == "reasoning" {
				signature = "reasoning_content"
			}
			index := s.ensureThinking(stream, output, signature)
			s.blocks[index].Thinking += reasoning
			output.Content = s.blocks
			stream.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: reasoning, Partial: output})
			break
		}
	}
	if toolCalls, ok := delta["tool_calls"].([]any); ok {
		for _, rawToolCall := range toolCalls {
			toolCall, _ := rawToolCall.(map[string]any)
			contentIndex := s.ensureToolCall(stream, output, toolCall)
			fn, _ := toolCall["function"].(map[string]any)
			deltaArgs, _ := fn["arguments"].(string)
			if name, ok := fn["name"].(string); ok && name != "" && s.blocks[contentIndex].Name == "" {
				s.blocks[contentIndex].Name = name
			}
			if id, ok := toolCall["id"].(string); ok && id != "" && s.blocks[contentIndex].ID == "" {
				s.blocks[contentIndex].ID = id
				s.toolContentByID[id] = contentIndex
			}
			if deltaArgs != "" {
				s.toolArgsByContent[contentIndex] += deltaArgs
				s.blocks[contentIndex].Arguments = parseJSONMap(s.toolArgsByContent[contentIndex])
			}
			output.Content = s.blocks
			stream.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: contentIndex, Delta: deltaArgs, Partial: output})
		}
	}
}

func (s *completionsStreamState) ensureText(stream *ai.AssistantMessageEventStream, output *ai.Message) int {
	if s.textIndex != nil {
		return *s.textIndex
	}
	s.blocks = append(s.blocks, ai.ContentBlock{Type: "text"})
	index := len(s.blocks) - 1
	s.textIndex = &index
	output.Content = s.blocks
	stream.Push(ai.AssistantMessageEvent{Type: "text_start", ContentIndex: index, Partial: output})
	return index
}

func (s *completionsStreamState) ensureThinking(stream *ai.AssistantMessageEventStream, output *ai.Message, signature string) int {
	if s.thinkingIndex != nil {
		return *s.thinkingIndex
	}
	s.blocks = append(s.blocks, ai.ContentBlock{Type: "thinking", ThinkingSignature: signature})
	index := len(s.blocks) - 1
	s.thinkingIndex = &index
	output.Content = s.blocks
	stream.Push(ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: index, Partial: output})
	return index
}

func (s *completionsStreamState) ensureToolCall(stream *ai.AssistantMessageEventStream, output *ai.Message, toolCall map[string]any) int {
	if rawIndex, ok := toolCall["index"].(float64); ok {
		streamIndex := int(rawIndex)
		if contentIndex, ok := s.toolContentByIndex[streamIndex]; ok {
			return contentIndex
		}
	}
	if id, ok := toolCall["id"].(string); ok && id != "" {
		if contentIndex, ok := s.toolContentByID[id]; ok {
			return contentIndex
		}
	}
	fn, _ := toolCall["function"].(map[string]any)
	name, _ := fn["name"].(string)
	id, _ := toolCall["id"].(string)
	s.blocks = append(s.blocks, ai.ContentBlock{Type: "toolCall", ID: id, Name: name, Arguments: map[string]any{}})
	contentIndex := len(s.blocks) - 1
	if rawIndex, ok := toolCall["index"].(float64); ok {
		s.toolContentByIndex[int(rawIndex)] = contentIndex
	}
	if id != "" {
		s.toolContentByID[id] = contentIndex
	}
	output.Content = s.blocks
	stream.Push(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: contentIndex, Partial: output})
	return contentIndex
}

func parseCompletionsUsageMap(rawUsage map[string]any, model ai.Model) ai.Usage {
	promptTokens := intFromAny(rawUsage["prompt_tokens"])
	completionTokens := intFromAny(rawUsage["completion_tokens"])
	cacheReadTokens := intFromAny(rawUsage["prompt_cache_hit_tokens"])
	cacheWriteTokens := 0
	if details, ok := rawUsage["prompt_tokens_details"].(map[string]any); ok {
		if cached := intFromAny(details["cached_tokens"]); cached != 0 {
			cacheReadTokens = cached
		}
		cacheWriteTokens = intFromAny(details["cache_write_tokens"])
	}
	input := promptTokens - cacheReadTokens - cacheWriteTokens
	if input < 0 {
		input = 0
	}
	usage := ai.Usage{
		Input:       input,
		Output:      completionTokens,
		CacheRead:   cacheReadTokens,
		CacheWrite:  cacheWriteTokens,
		TotalTokens: input + completionTokens + cacheReadTokens + cacheWriteTokens,
	}
	ai.CalculateCost(model, &usage)
	return usage
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		value, _ := typed.Int64()
		return int(value)
	default:
		return 0
	}
}

func mapStopReason(reason string) ai.StopReason {
	switch reason {
	case "", "stop", "end":
		return ai.StopReasonStop
	case "length":
		return ai.StopReasonLength
	case "function_call", "tool_calls":
		return ai.StopReasonToolUse
	default:
		return ai.StopReasonError
	}
}

func clampPromptCacheKey(value string) string {
	return ClampOpenAIPromptCacheKey(value)
}

func resolveCacheRetention(value ai.CacheRetention) ai.CacheRetention {
	if value != "" {
		return value
	}
	if os.Getenv("PI_CACHE_RETENTION") == "long" {
		return ai.CacheRetentionLong
	}
	return ai.CacheRetentionShort
}

func promptCacheKey(model ai.Model, sessionID string, retention ai.CacheRetention, supportsLong bool) string {
	if sessionID == "" || retention == ai.CacheRetentionNone {
		return ""
	}
	if strings.Contains(model.BaseURL, "api.openai.com") || (retention == ai.CacheRetentionLong && supportsLong) {
		return clampPromptCacheKey(sessionID)
	}
	return ""
}

func applyCompletionsThinkingParams(params map[string]any, model ai.Model, effort *ai.ThinkingLevel, compat ResolvedOpenAICompletionsCompat) {
	if !model.Reasoning {
		return
	}
	enabled := effort != nil
	switch compat.ThinkingFormat {
	case "zai", "qwen":
		params["enable_thinking"] = enabled
	case "qwen-chat-template":
		params["chat_template_kwargs"] = map[string]any{"enable_thinking": enabled, "preserve_thinking": true}
	case "deepseek":
		if enabled {
			params["thinking"] = map[string]any{"type": "enabled"}
			params["reasoning_effort"] = mappedThinkingLevel(model, *effort)
		} else {
			params["thinking"] = map[string]any{"type": "disabled"}
		}
	case "openrouter":
		if enabled {
			params["reasoning"] = map[string]any{"effort": mappedThinkingLevel(model, *effort)}
		} else if off := mappedOffThinkingLevel(model); off != "" {
			params["reasoning"] = map[string]any{"effort": off}
		}
	case "together":
		params["reasoning"] = map[string]any{"enabled": enabled}
		if enabled && compat.SupportsReasoningEffort {
			params["reasoning_effort"] = mappedThinkingLevel(model, *effort)
		}
	case "a8c":
		if enabled && ai.ModelThinkingLevel(*effort) != ai.ModelThinkingLevelOff {
			params["include_reasoning"] = true
			params["reasoning_effort"] = mappedA8CThinkingLevel(model, *effort)
		} else {
			params["include_reasoning"] = false
		}
	default:
		if enabled && compat.SupportsReasoningEffort {
			params["reasoning_effort"] = mappedThinkingLevel(model, *effort)
		} else if off := mappedOffThinkingLevel(model); off != "" && compat.SupportsReasoningEffort {
			params["reasoning_effort"] = off
		}
	}
}

func mappedThinkingLevel(model ai.Model, level ai.ThinkingLevel) string {
	if model.ThinkingLevelMap != nil {
		if value, ok := model.ThinkingLevelMap[ai.ModelThinkingLevel(level)]; ok && value != nil {
			return *value
		}
	}
	return string(level)
}

func mappedA8CThinkingLevel(model ai.Model, level ai.ThinkingLevel) string {
	mapped := mappedThinkingLevel(model, level)
	if mapped == string(ai.ThinkingLevelMinimal) {
		return string(ai.ThinkingLevelLow)
	}
	return mapped
}

func simpleReasoningEffort(model ai.Model, requested *ai.ThinkingLevel) *ai.ThinkingLevel {
	if requested == nil {
		return nil
	}
	clamped := ai.ClampThinkingLevel(model, ai.ModelThinkingLevel(*requested))
	if clamped == ai.ModelThinkingLevelOff {
		return nil
	}
	effort := ai.ThinkingLevel(clamped)
	return &effort
}

func mappedOffThinkingLevel(model ai.Model) string {
	if model.ThinkingLevelMap != nil {
		if value, ok := model.ThinkingLevelMap[ai.ModelThinkingLevelOff]; ok {
			if value == nil {
				return ""
			}
			return *value
		}
	}
	return "none"
}

func hasToolHistory(messages []ai.Message) bool {
	for _, msg := range messages {
		if msg.Role == "toolResult" {
			return true
		}
		if msg.Role == "assistant" {
			for _, block := range contentBlocks(msg.Content) {
				if block.Type == "toolCall" {
					return true
				}
			}
		}
	}
	return false
}

type responsesStreamState struct {
	blocks                  []ai.ContentBlock
	currentIndex            int
	currentItemID           string
	currentItemType         string
	currentItem             map[string]any
	currentMessagePartType  string
	hasReasoningSummaryPart bool
	itemIndexByID           map[string]int
	itemIDByOutputIndex     map[int]string
	itemTypeByID            map[string]string
	messagePartTypeByID     map[string]string
	reasoningSummaryByID    map[string]bool
	toolArgsByIndex         map[int]string
	toolArgsByItemID        map[string]string
	nativeToolsByItemID     map[string]ai.ToolCall
}

func newResponsesStreamState() *responsesStreamState {
	return &responsesStreamState{
		currentIndex:         -1,
		itemIndexByID:        map[string]int{},
		itemIDByOutputIndex:  map[int]string{},
		itemTypeByID:         map[string]string{},
		messagePartTypeByID:  map[string]string{},
		reasoningSummaryByID: map[string]bool{},
		toolArgsByIndex:      map[int]string{},
		toolArgsByItemID:     map[string]string{},
		nativeToolsByItemID:  map[string]ai.ToolCall{},
	}
}

func responsesItemID(item map[string]any) string {
	return strings.TrimSpace(stringFromAny(item["id"]))
}

func responsesEventItemID(event map[string]any) string {
	return strings.TrimSpace(stringFromAny(event["item_id"]))
}

func (s *responsesStreamState) eventItemID(event map[string]any) string {
	if id := responsesEventItemID(event); id != "" {
		return id
	}
	if outputIndex, ok := event["output_index"]; ok {
		if id := s.itemIDByOutputIndex[intFromAny(outputIndex)]; id != "" {
			return id
		}
	}
	return s.currentItemID
}

func (s *responsesStreamState) eventContentIndex(event map[string]any) int {
	itemID := s.eventItemID(event)
	if itemID != "" {
		if index, ok := s.itemIndexByID[itemID]; ok {
			return index
		}
	}
	return s.currentIndex
}

func (s *responsesStreamState) eventItemType(event map[string]any) string {
	itemID := s.eventItemID(event)
	if itemID != "" {
		if itemType := s.itemTypeByID[itemID]; itemType != "" {
			return itemType
		}
	}
	return s.currentItemType
}

func (s *responsesStreamState) setItemIndex(itemID, itemType string, index int) {
	if itemID == "" {
		return
	}
	s.itemIndexByID[itemID] = index
	s.itemTypeByID[itemID] = itemType
}

func (s *responsesStreamState) setEventOutputIndex(event map[string]any, itemID string) {
	if itemID == "" {
		return
	}
	if outputIndex, ok := event["output_index"]; ok {
		s.itemIDByOutputIndex[intFromAny(outputIndex)] = itemID
	}
}

func appendMissingPrefix(current, full string) string {
	if full == "" || full == current {
		return ""
	}
	if current == "" {
		return full
	}
	if strings.HasPrefix(full, current) {
		return full[len(current):]
	}
	return ""
}

func responsesPartText(part map[string]any, partType string) string {
	switch partType {
	case "refusal":
		return stringFromAny(part["refusal"])
	default:
		return stringFromAny(part["text"])
	}
}

func responsesNativeToolCall(item map[string]any, fallbackIndex int, provider ai.Provider) (ai.ToolCall, bool) {
	name, _, ok := responsesNativeToolInfo(item, provider)
	if !ok {
		return ai.ToolCall{}, false
	}
	id := responsesItemID(item)
	if id == "" {
		id = fmt.Sprintf("native_%s_%d", strings.ReplaceAll(name, "_", "-"), fallbackIndex+1)
	}
	return ai.ToolCall{
		Type:      "toolCall",
		ID:        id,
		Name:      name,
		Arguments: responsesNativeToolArguments(item, name),
	}, true
}

func responsesNativeToolInfo(item map[string]any, provider ai.Provider) (toolName string, resultProvider string, ok bool) {
	itemType := strings.TrimSpace(stringFromAny(item["type"]))
	switch itemType {
	case "web_search_call":
		return "web_search", string(provider), true
	case "openrouter:web_search":
		return "web_search", string(ai.ProviderOpenRouter), true
	case "openrouter:web_fetch":
		return "fetch", string(ai.ProviderOpenRouter), true
	default:
		return "", "", false
	}
}

func responsesNativeToolArguments(item map[string]any, toolName string) map[string]any {
	args := map[string]any{}
	switch toolName {
	case "web_search":
		addNativeWebSearchQuery(args, item)
		if action, _ := item["action"].(map[string]any); action != nil {
			addNativeWebSearchQuery(args, action)
			if queries, ok := action["queries"].([]any); ok && len(queries) > 0 {
				args["queries"] = queries
			}
			if actionType := strings.TrimSpace(stringFromAny(action["type"])); actionType != "" {
				args["action"] = actionType
			}
		}
	case "fetch":
		if url := strings.TrimSpace(stringFromAny(item["url"])); url != "" {
			args["url"] = url
		}
	}
	return args
}

func addNativeWebSearchQuery(args map[string]any, data map[string]any) {
	if _, ok := args["query"]; ok {
		return
	}
	for _, key := range []string{"query", "search_query", "searchQuery"} {
		if value := strings.TrimSpace(stringFromAny(data[key])); value != "" {
			args["query"] = value
			return
		}
	}
}

func responsesNativeToolResult(item map[string]any, provider ai.Provider) map[string]any {
	toolName, resultProvider, _ := responsesNativeToolInfo(item, provider)
	status := strings.TrimSpace(stringFromAny(item["status"]))
	result := map[string]any{
		"state":    "complete",
		"status":   "success",
		"provider": resultProvider,
		"native":   true,
	}
	if status != "" {
		result["providerStatus"] = status
	}
	for _, key := range []string{"url", "title", "httpStatus", "http_status"} {
		if value := item[key]; value != nil {
			result[key] = value
		}
	}
	if toolName == "fetch" {
		if url := stringFromAny(item["url"]); url != "" {
			result["final_url"] = url
		}
	}
	if args := responsesNativeToolArguments(item, toolName); len(args) > 0 {
		for key, value := range args {
			result[key] = value
		}
	}
	if status == "failed" || status == "incomplete" {
		result["state"] = "error"
		result["status"] = "failed"
		if errorData, _ := item["error"].(map[string]any); errorData != nil {
			if message := strings.TrimSpace(stringFromAny(errorData["message"])); message != "" {
				result["reason"] = message
			}
		}
		if message := strings.TrimSpace(stringFromAny(item["error"])); message != "" {
			result["reason"] = message
		}
		if result["reason"] == nil {
			result["reason"] = "Provider-native web tool failed"
		}
	}
	return result
}

func (s *responsesStreamState) apply(stream *ai.AssistantMessageEventStream, output *ai.Message, model ai.Model, options OpenAIResponsesOptions, event map[string]any) {
	eventType, _ := event["type"].(string)
	push := func(evt ai.AssistantMessageEvent) {
		evt.RawEvent = event
		evt.RawSource = string(model.Provider)
		stream.Push(evt)
	}
	push(ai.AssistantMessageEvent{Type: "raw", Partial: output})
	switch eventType {
	case "response.created":
		if response, ok := event["response"].(map[string]any); ok {
			if id, ok := response["id"].(string); ok {
				output.ResponseID = id
			}
		}
	case "response.output_item.added":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		itemID := responsesItemID(item)
		s.currentItem = item
		s.currentItemID = itemID
		s.setEventOutputIndex(event, itemID)
		s.currentItemType = itemType
		s.currentMessagePartType = ""
		s.hasReasoningSummaryPart = false
		switch itemType {
		case "reasoning":
			s.blocks = append(s.blocks, ai.ContentBlock{Type: "thinking"})
			s.currentIndex = len(s.blocks) - 1
			s.setItemIndex(itemID, itemType, s.currentIndex)
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: s.currentIndex, Partial: output})
		case "message":
			s.blocks = append(s.blocks, ai.ContentBlock{Type: "text"})
			s.currentIndex = len(s.blocks) - 1
			s.setItemIndex(itemID, itemType, s.currentIndex)
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "text_start", ContentIndex: s.currentIndex, Partial: output})
		case "function_call":
			id := fmt.Sprintf("%v|%v", item["call_id"], item["id"])
			arguments := ""
			if rawArguments, ok := item["arguments"]; ok && rawArguments != nil {
				if text, ok := rawArguments.(string); ok {
					arguments = text
				} else {
					arguments = mustJSON(rawArguments)
				}
			}
			s.blocks = append(s.blocks, ai.ContentBlock{Type: "toolCall", ID: id, Name: fmt.Sprint(item["name"]), Arguments: parseJSONMap(arguments)})
			s.currentIndex = len(s.blocks) - 1
			s.setItemIndex(itemID, itemType, s.currentIndex)
			s.toolArgsByIndex[s.currentIndex] = arguments
			if itemID != "" {
				s.toolArgsByItemID[itemID] = arguments
			}
			output.Content = s.blocks
			toolCall := ai.ToolCall{Type: "toolCall", ID: id, Name: s.blocks[s.currentIndex].Name, Arguments: s.blocks[s.currentIndex].Arguments}
			push(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: s.currentIndex, ToolCall: &toolCall, Partial: output})
			if arguments != "" {
				push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: s.currentIndex, Delta: arguments, ToolCall: &toolCall, Partial: output})
			}
		case "image_generation_call":
			s.blocks = append(s.blocks, imageBlockFromGenerationItem(item, ai.ContentBlock{}))
			s.currentIndex = len(s.blocks) - 1
			s.setItemIndex(itemID, itemType, s.currentIndex)
			output.Content = s.blocks
		case "web_search_call", "openrouter:web_search", "openrouter:web_fetch":
			toolCall, ok := responsesNativeToolCall(item, len(s.nativeToolsByItemID), model.Provider)
			if !ok {
				return
			}
			s.nativeToolsByItemID[itemID] = toolCall
			s.currentIndex = -1
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "toolcall_start", ToolCall: &toolCall, Partial: output})
			if len(toolCall.Arguments) > 0 {
				push(ai.AssistantMessageEvent{Type: "toolcall_delta", Delta: mustJSON(toolCall.Arguments), ToolCall: &toolCall, Partial: output})
			}
		}
	case "response.reasoning_summary_part.added":
		itemID := s.eventItemID(event)
		if s.eventItemType(event) == "reasoning" {
			if itemID != "" {
				s.reasoningSummaryByID[itemID] = true
			}
			if itemID == "" || itemID == s.currentItemID {
				s.hasReasoningSummaryPart = true
			}
		}
	case "response.reasoning_summary_text.delta":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		hasSummaryPart := s.hasReasoningSummaryPart
		if itemID != "" {
			hasSummaryPart = s.reasoningSummaryByID[itemID]
		}
		if hasSummaryPart && index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "thinking" {
			delta, _ := event["delta"].(string)
			s.blocks[index].Thinking += delta
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: delta, Partial: output})
		}
	case "response.reasoning_text.delta":
		index := s.eventContentIndex(event)
		if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "thinking" {
			delta, _ := event["delta"].(string)
			s.blocks[index].Thinking += delta
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: delta, Partial: output})
		}
	case "response.reasoning_text.done":
		index := s.eventContentIndex(event)
		if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "thinking" {
			text, _ := event["text"].(string)
			if delta := appendMissingPrefix(s.blocks[index].Thinking, text); delta != "" {
				s.blocks[index].Thinking += delta
				output.Content = s.blocks
				push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: delta, Partial: output})
			}
		}
	case "response.reasoning_summary_part.done":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		hasSummaryPart := s.hasReasoningSummaryPart
		if itemID != "" {
			hasSummaryPart = s.reasoningSummaryByID[itemID]
		}
		if hasSummaryPart && index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "thinking" {
			if part, ok := event["part"].(map[string]any); ok {
				if delta := appendMissingPrefix(s.blocks[index].Thinking, responsesPartText(part, "summary_text")); delta != "" {
					s.blocks[index].Thinking += delta
					output.Content = s.blocks
					push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: delta, Partial: output})
				}
			}
			if s.blocks[index].Thinking == "" || strings.HasSuffix(s.blocks[index].Thinking, "\n\n") {
				return
			}
			s.blocks[index].Thinking += "\n\n"
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: "\n\n", Partial: output})
		}
	case "response.reasoning_summary_text.done":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		hasSummaryPart := s.hasReasoningSummaryPart
		if itemID != "" {
			hasSummaryPart = s.reasoningSummaryByID[itemID]
		}
		if hasSummaryPart && index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "thinking" {
			text, _ := event["text"].(string)
			if delta := appendMissingPrefix(s.blocks[index].Thinking, text); delta != "" {
				s.blocks[index].Thinking += delta
				output.Content = s.blocks
				push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: delta, Partial: output})
			}
		}
	case "response.content_part.added":
		itemID := s.eventItemID(event)
		if s.eventItemType(event) == "message" {
			if part, ok := event["part"].(map[string]any); ok {
				partType, _ := part["type"].(string)
				if partType == "output_text" || partType == "refusal" {
					if itemID != "" {
						s.messagePartTypeByID[itemID] = partType
					}
					if itemID == "" || itemID == s.currentItemID {
						s.currentMessagePartType = partType
					}
				}
			}
		}
	case "response.output_text.delta":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		partType := s.currentMessagePartType
		if itemID != "" {
			partType = s.messagePartTypeByID[itemID]
		}
		if s.eventItemType(event) == "message" && partType == "" {
			partType = "output_text"
			if itemID != "" {
				s.messagePartTypeByID[itemID] = partType
			}
			if itemID == "" || itemID == s.currentItemID {
				s.currentMessagePartType = partType
			}
		}
		if partType == "output_text" && index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "text" {
			delta, _ := event["delta"].(string)
			s.blocks[index].Text += delta
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: delta, Partial: output})
		}
	case "response.output_text.done":
		index := s.eventContentIndex(event)
		if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "text" {
			text, _ := event["text"].(string)
			if delta := appendMissingPrefix(s.blocks[index].Text, text); delta != "" {
				s.blocks[index].Text += delta
				output.Content = s.blocks
				push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: delta, Partial: output})
			}
		}
	case "response.refusal.delta":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		partType := s.currentMessagePartType
		if itemID != "" {
			partType = s.messagePartTypeByID[itemID]
		}
		if s.eventItemType(event) == "message" && partType == "" {
			partType = "refusal"
			if itemID != "" {
				s.messagePartTypeByID[itemID] = partType
			}
			if itemID == "" || itemID == s.currentItemID {
				s.currentMessagePartType = partType
			}
		}
		if partType == "refusal" && index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "text" {
			delta, _ := event["delta"].(string)
			s.blocks[index].Text += delta
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: delta, Partial: output})
		}
	case "response.refusal.done":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		if itemID != "" {
			s.messagePartTypeByID[itemID] = "refusal"
		}
		if itemID == "" || itemID == s.currentItemID {
			s.currentMessagePartType = "refusal"
		}
		if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "text" {
			refusal, _ := event["refusal"].(string)
			if delta := appendMissingPrefix(s.blocks[index].Text, refusal); delta != "" {
				s.blocks[index].Text += delta
				output.Content = s.blocks
				push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: delta, Partial: output})
			}
		}
	case "response.content_part.done":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		part, _ := event["part"].(map[string]any)
		partType, _ := part["type"].(string)
		switch partType {
		case "output_text", "refusal":
			if itemID != "" {
				s.messagePartTypeByID[itemID] = partType
			}
			if itemID == "" || itemID == s.currentItemID {
				s.currentMessagePartType = partType
			}
			if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "text" {
				if delta := appendMissingPrefix(s.blocks[index].Text, responsesPartText(part, partType)); delta != "" {
					s.blocks[index].Text += delta
					output.Content = s.blocks
					push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: delta, Partial: output})
				}
			}
		case "reasoning_text":
			if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "thinking" {
				if delta := appendMissingPrefix(s.blocks[index].Thinking, responsesPartText(part, partType)); delta != "" {
					s.blocks[index].Thinking += delta
					output.Content = s.blocks
					push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: delta, Partial: output})
				}
			}
		}
	case "response.output_text.annotation.added":
		contentIndex := s.eventContentIndex(event)
		if contentIndex < 0 {
			contentIndex = intFromAny(event["content_index"])
		}
		if annotation, ok := event["annotation"].(map[string]any); ok {
			output.Citations = append(output.Citations, providerCitationsFromAny(annotation, model.Provider, contentIndex)...)
			push(ai.AssistantMessageEvent{Type: "source", Partial: output})
		}
	case "response.function_call_arguments.delta":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "toolCall" {
			delta, _ := event["delta"].(string)
			if itemID != "" {
				s.toolArgsByItemID[itemID] += delta
				s.blocks[index].Arguments = parseJSONMap(s.toolArgsByItemID[itemID])
			} else {
				s.toolArgsByIndex[index] += delta
				s.blocks[index].Arguments = parseJSONMap(s.toolArgsByIndex[index])
			}
			output.Content = s.blocks
			toolCall := ai.ToolCall{Type: "toolCall", ID: s.blocks[index].ID, Name: s.blocks[index].Name, Arguments: s.blocks[index].Arguments}
			push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: index, Delta: delta, ToolCall: &toolCall, Partial: output})
		}
	case "response.function_call_arguments.done":
		itemID := s.eventItemID(event)
		index := s.eventContentIndex(event)
		if index >= 0 && index < len(s.blocks) && s.blocks[index].Type == "toolCall" {
			args, _ := event["arguments"].(string)
			previous := s.toolArgsByIndex[index]
			if itemID != "" {
				previous = s.toolArgsByItemID[itemID]
				s.toolArgsByItemID[itemID] = args
			}
			s.toolArgsByIndex[index] = args
			s.blocks[index].Arguments = parseJSONMap(args)
			output.Content = s.blocks
			if strings.HasPrefix(args, previous) && len(args) > len(previous) {
				toolCall := ai.ToolCall{Type: "toolCall", ID: s.blocks[index].ID, Name: s.blocks[index].Name, Arguments: s.blocks[index].Arguments}
				push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: index, Delta: args[len(previous):], ToolCall: &toolCall, Partial: output})
			}
		}
	case "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		itemID := responsesItemID(item)
		if _, _, ok := responsesNativeToolInfo(item, model.Provider); ok {
			toolCall := s.nativeToolsByItemID[itemID]
			if toolCall.ID == "" {
				toolCall, _ = responsesNativeToolCall(item, len(s.nativeToolsByItemID), model.Provider)
			} else if args := responsesNativeToolArguments(item, toolCall.Name); len(args) > 0 {
				toolCall.Arguments = args
			}
			output.Content = s.blocks
			if citations := providerCitationsFromAny(item, model.Provider, max(0, s.currentIndex)); len(citations) > 0 {
				output.Citations = append(output.Citations, citations...)
				push(ai.AssistantMessageEvent{Type: "source", Partial: output})
			}
			push(ai.AssistantMessageEvent{Type: "toolresult", ToolCall: &toolCall, CustomValue: responsesNativeToolResult(item, model.Provider), Partial: output})
			s.currentIndex = -1
			return
		}
		index := s.currentIndex
		if itemID != "" {
			if storedIndex, ok := s.itemIndexByID[itemID]; ok {
				index = storedIndex
			}
		}
		if index < 0 && itemType != "image_generation_call" {
			return
		}
		switch itemType {
		case "reasoning":
			s.blocks[index].Thinking = reasoningTextFromItem(item, s.blocks[index].Thinking)
			s.blocks[index].ThinkingSignature = mustJSON(item)
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: index, Content: s.blocks[index].Thinking, Partial: output})
			s.currentIndex = -1
		case "message":
			if text := messageTextFromItem(item); text != "" {
				s.blocks[index].Text = text
			}
			output.Citations = append(output.Citations, providerCitationsFromAny(item, model.Provider, index)...)
			if id, ok := item["id"].(string); ok && id != "" {
				payload := map[string]any{"v": 1, "id": id}
				if phase, ok := item["phase"].(string); ok && phase != "" {
					payload["phase"] = phase
				}
				s.blocks[index].TextSignature = mustJSON(payload)
			}
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: index, Content: s.blocks[index].Text, Partial: output})
			s.currentIndex = -1
		case "function_call":
			if s.blocks[index].Name == "" {
				s.blocks[index].Name = fmt.Sprint(item["name"])
			}
			if args, ok := item["arguments"].(string); ok && args != "" {
				s.blocks[index].Arguments = parseJSONMap(args)
			}
			toolCall := ai.ToolCall{Type: "toolCall", ID: s.blocks[index].ID, Name: s.blocks[index].Name, Arguments: s.blocks[index].Arguments}
			output.Content = s.blocks
			push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: index, ToolCall: &toolCall, Partial: output})
			s.currentIndex = -1
		case "image_generation_call":
			if index < 0 || index >= len(s.blocks) || s.blocks[index].Type != "image" {
				s.blocks = append(s.blocks, imageBlockFromGenerationItem(item, ai.ContentBlock{}))
				s.currentIndex = len(s.blocks) - 1
			} else {
				s.blocks[index] = imageBlockFromGenerationItem(item, s.blocks[index])
			}
			output.Content = s.blocks
			s.currentIndex = -1
		}
	case "response.completed":
		response, _ := event["response"].(map[string]any)
		if id, ok := response["id"].(string); ok {
			output.ResponseID = id
		}
		if usage, ok := response["usage"].(map[string]any); ok {
			output.Usage = parseResponsesUsageMap(usage, model)
		}
		status, _ := response["status"].(string)
		output.StopReason = mapResponsesStopReason(status)
		for _, block := range s.blocks {
			if block.Type == "toolCall" && output.StopReason == ai.StopReasonStop {
				output.StopReason = ai.StopReasonToolUse
				break
			}
		}
		serviceTier := options.ServiceTier
		if responseTier, ok := response["service_tier"].(string); ok && responseTier != "" {
			serviceTier = responseTier
		}
		applyServiceTierPricing(&output.Usage, model, serviceTier)
	case "response.failed":
		output.StopReason = ai.StopReasonError
		output.ErrorMessage = responseFailedMessage(event)
	case "error":
		output.StopReason = ai.StopReasonError
		output.ErrorMessage = fmt.Sprintf("Error Code %v: %v", event["code"], event["message"])
	}
}

func reasoningTextFromItem(item map[string]any, fallback string) string {
	for _, key := range []string{"summary", "content"} {
		if parts, ok := item[key].([]any); ok {
			texts := []string{}
			for _, raw := range parts {
				part, _ := raw.(map[string]any)
				if text, ok := part["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
			if len(texts) > 0 {
				return strings.Join(texts, "\n\n")
			}
		}
	}
	return fallback
}

func messageTextFromItem(item map[string]any) string {
	content, _ := item["content"].([]any)
	parts := []string{}
	for _, raw := range content {
		part, _ := raw.(map[string]any)
		if text, ok := part["text"].(string); ok {
			parts = append(parts, text)
		} else if refusal, ok := part["refusal"].(string); ok {
			parts = append(parts, refusal)
		}
	}
	return strings.Join(parts, "")
}

func imageBlockFromGenerationItem(item map[string]any, fallback ai.ContentBlock) ai.ContentBlock {
	block := fallback
	block.Type = "image"
	if block.MimeType == "" {
		block.MimeType = "image/png"
	}
	if block.Name == "" {
		block.Name = "image.png"
	}
	if id, ok := item["id"].(string); ok && id != "" {
		block.ID = id
	}
	if result, ok := item["result"].(string); ok && result != "" {
		block.MimeType, block.Data = normalizeGeneratedImageData(result, block.MimeType)
	}
	return block
}

func normalizeGeneratedImageData(data string, fallbackMime string) (string, string) {
	if prefix, value, ok := strings.Cut(data, ","); ok && strings.Contains(prefix, ";base64") {
		mimeType := strings.TrimPrefix(strings.Split(prefix, ";")[0], "data:")
		if mimeType == "" {
			mimeType = fallbackMime
		}
		return mimeType, value
	}
	return fallbackMime, data
}

func parseResponsesUsageMap(rawUsage map[string]any, model ai.Model) ai.Usage {
	inputTokens := intFromAny(rawUsage["input_tokens"])
	outputTokens := intFromAny(rawUsage["output_tokens"])
	totalTokens := intFromAny(rawUsage["total_tokens"])
	cachedTokens := 0
	if details, ok := rawUsage["input_tokens_details"].(map[string]any); ok {
		cachedTokens = intFromAny(details["cached_tokens"])
	}
	reasoningTokens := 0
	if details, ok := rawUsage["output_tokens_details"].(map[string]any); ok {
		reasoningTokens = intFromAny(details["reasoning_tokens"])
	}
	input := inputTokens - cachedTokens
	if input < 0 {
		input = 0
	}
	usage := ai.Usage{Input: input, Output: outputTokens, CacheRead: cachedTokens, ReasoningTokens: reasoningTokens, TotalTokens: totalTokens}
	ai.CalculateCost(model, &usage)
	return usage
}

func mapResponsesStopReason(status string) ai.StopReason {
	switch status {
	case "", "completed", "in_progress", "queued":
		return ai.StopReasonStop
	case "incomplete":
		return ai.StopReasonLength
	case "failed", "cancelled":
		return ai.StopReasonError
	default:
		return ai.StopReasonError
	}
}

func responseFailedMessage(event map[string]any) string {
	response, _ := event["response"].(map[string]any)
	if errorPayload, ok := response["error"].(map[string]any); ok {
		return fmt.Sprintf("%v: %v", errorPayload["code"], errorPayload["message"])
	}
	if details, ok := response["incomplete_details"].(map[string]any); ok {
		if reason, ok := details["reason"].(string); ok {
			return "incomplete: " + reason
		}
	}
	return "Unknown error (no error details in response)"
}

func applyServiceTierPricing(usage *ai.Usage, model ai.Model, serviceTier string) {
	multiplier := 1.0
	switch serviceTier {
	case "flex":
		multiplier = 0.5
	case "priority":
		if model.ID == "gpt-5.5" {
			multiplier = 2.5
		} else {
			multiplier = 2
		}
	}
	if multiplier == 1 {
		return
	}
	usage.Cost.Input *= multiplier
	usage.Cost.Output *= multiplier
	usage.Cost.CacheRead *= multiplier
	usage.Cost.CacheWrite *= multiplier
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}
