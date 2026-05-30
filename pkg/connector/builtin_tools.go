package connector

import (
	"context"
	"strings"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/msgconv"
)

func (cl *Client) registerProviderBuiltInToolHooks(agentHarness *harness.AgentHarness, provider aiid.ProviderConfig, model ai.Model, prompt msgconv.MatrixPrompt) {
	if agentHarness == nil {
		return
	}
	agentHarness.On("before_provider_payload", func(_ context.Context, event harness.AgentHarnessEvent) (any, error) {
		if event.Model == nil {
			return nil, nil
		}
		payload, changed := providerBuiltInToolsPayload(provider, model, *event.Model, prompt, event.Payload)
		if !changed {
			return nil, nil
		}
		return harness.BeforeProviderPayloadResult{Payload: payload}, nil
	})
}

func providerBuiltInToolsPayload(provider aiid.ProviderConfig, selectedModel ai.Model, payloadModel ai.Model, prompt msgconv.MatrixPrompt, payload any) (any, bool) {
	body, ok := payload.(map[string]any)
	if !ok || !shouldUseImageGenerationBuiltIn(payloadModel, selectedModel, prompt) {
		return payload, false
	}
	toolType := imageGenerationBuiltInToolType(payloadModel)
	if toolType == "" {
		return payload, false
	}

	next := clonePayloadMap(body)
	next["tools"] = appendBuiltInTool(next["tools"], toolType)
	if toolType == "image_generation" && isOpenAIImageGenerationModelID(payloadModel.ID) {
		hostModelID, ok := openAIImageGenerationHostModelID(provider, payloadModel.ID)
		if !ok {
			return payload, false
		}
		next["model"] = hostModelID
		next["tool_choice"] = map[string]any{"type": "image_generation"}
	}
	return next, true
}

func shouldUseImageGenerationBuiltIn(payloadModel ai.Model, selectedModel ai.Model, prompt msgconv.MatrixPrompt) bool {
	return promptRequestsImageGeneration(prompt) || isOpenAIImageGenerationModelID(payloadModel.ID) || isOpenAIImageGenerationModelID(selectedModel.ID)
}

func imageGenerationBuiltInToolType(model ai.Model) string {
	switch model.Provider {
	case ai.ProviderOpenAI:
		if model.API == ai.ApiOpenAIResponses && (modelSupportsBuiltInTool(model, "image_generation") || isOpenAIImageGenerationModelID(model.ID)) {
			return "image_generation"
		}
	case ai.ProviderOpenRouter:
		if (model.API == ai.ApiOpenAIResponses || model.API == ai.ApiOpenAICompletions) && modelSupportsBuiltInTool(model, "openrouter:image_generation") {
			return "openrouter:image_generation"
		}
	}
	return ""
}

func openAIImageGenerationHostModelID(provider aiid.ProviderConfig, fallback string) (string, bool) {
	for _, model := range provider.Models {
		if model.Provider == ai.ProviderOpenAI && model.API == ai.ApiOpenAIResponses && modelSupportsBuiltInTool(model, "image_generation") {
			return model.ID, true
		}
	}
	if len(provider.Models) == 0 && strings.HasPrefix(fallback, "openai/") {
		return "openai/gpt-5.5", true
	}
	if len(provider.Models) == 0 {
		return "gpt-5.5", true
	}
	return "", false
}

func modelSupportsBuiltInTool(model ai.Model, toolType string) bool {
	for _, tool := range model.BuiltInTools {
		if tool == toolType {
			return true
		}
	}
	return false
}

func isOpenAIImageGenerationModelID(modelID string) bool {
	modelID = strings.TrimPrefix(modelID, "openai/")
	return strings.HasPrefix(modelID, "gpt-image-")
}

func promptRequestsImageGeneration(prompt msgconv.MatrixPrompt) bool {
	text := strings.ToLower(strings.TrimSpace(prompt.Text))
	if text == "" {
		return false
	}
	imageNouns := []string{"image", "picture", "photo", "illustration", "artwork", "poster", "wallpaper", "logo", "icon", "avatar"}
	createVerbs := []string{"create", "generate", "draw", "render", "make", "design", "paint", "illustrate"}
	editVerbs := []string{"edit", "modify", "change", "replace", "remove", "add", "upscale", "enhance", "retouch", "turn this", "make it look"}
	if containsAny(text, imageNouns) && containsAny(text, append(createVerbs, editVerbs...)) {
		return true
	}
	for _, prefix := range []string{"draw ", "generate ", "create ", "make me ", "make an ", "make a ", "render "} {
		if strings.HasPrefix(text, prefix) && containsAny(text, imageNouns) {
			return true
		}
	}
	if hasImageAttachment(prompt) && containsAny(text, editVerbs) {
		return true
	}
	return false
}

func hasImageAttachment(prompt msgconv.MatrixPrompt) bool {
	for _, attachment := range prompt.Attachments {
		if attachment.Type == "image" || strings.HasPrefix(attachment.MimeType, "image/") {
			return true
		}
	}
	return false
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func appendBuiltInTool(raw any, toolType string) []any {
	tools := toolsAsAny(raw)
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]any); ok && toolMap["type"] == toolType {
			return tools
		}
	}
	return append(tools, map[string]any{"type": toolType})
}

func toolsAsAny(raw any) []any {
	switch tools := raw.(type) {
	case []any:
		return append([]any{}, tools...)
	case []map[string]any:
		out := make([]any, 0, len(tools))
		for _, tool := range tools {
			out = append(out, tool)
		}
		return out
	default:
		return nil
	}
}

func clonePayloadMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
