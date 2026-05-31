package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
)

type GoogleOptions struct {
	ai.StreamOptions
	ToolChoice string
	Thinking   *GoogleThinkingOptions
}

func StreamSimpleGoogle(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	if options.APIKey == "" {
		options.APIKey = getEnvAPIKey(model.Provider)
	}
	base := BuildBaseOptions(model, &options, options.APIKey)
	if options.Reasoning == nil {
		return StreamGoogle(ctx, model, llmContext, GoogleOptions{
			StreamOptions: base,
			Thinking:      &GoogleThinkingOptions{Enabled: false},
		})
	}
	clamped := ai.ClampThinkingLevel(model, ai.ModelThinkingLevel(*options.Reasoning))
	effort := ai.ThinkingLevel(clamped)
	if clamped == ai.ModelThinkingLevelOff {
		effort = ai.ThinkingLevelHigh
	}
	if isGoogleGemini3ProModel(model) || isGoogleGemini3FlashModel(model) || isGoogleGemma4Model(model) {
		return StreamGoogle(ctx, model, llmContext, GoogleOptions{
			StreamOptions: base,
			Thinking:      &GoogleThinkingOptions{Enabled: true, Level: getGoogleThinkingLevel(effort, model)},
		})
	}
	budget := getGoogleBudget(model, effort, options.ThinkingBudgets)
	return StreamGoogle(ctx, model, llmContext, GoogleOptions{
		StreamOptions: base,
		Thinking:      &GoogleThinkingOptions{Enabled: true, BudgetTokens: &budget},
	})
}

func StreamGoogle(ctx context.Context, model ai.Model, llmContext ai.Context, options GoogleOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		output := newAssistant(model)
		if options.APIKey == "" {
			pushFinalError(stream, &output, "No API key for provider: "+string(model.Provider))
			return
		}
		params := BuildGoogleParams(model, llmContext, options)
		if options.OnPayload != nil {
			if next, ok, err := options.OnPayload(params, model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			} else if ok {
				nextParams, ok := next.(map[string]any)
				if !ok {
					pushFinalError(stream, &output, "onPayload returned unsupported Google request body")
					return
				}
				params = nextParams
			}
		}
		response, err := doGoogleRequest(ctx, model, options, params)
		if err != nil {
			pushFinalError(stream, &output, err.Error())
			return
		}
		defer response.Body.Close()
		if options.OnResponse != nil {
			if err := options.OnResponse(providerResponse(response), model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			}
		}
		stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
		state := newGoogleVertexStreamState()
		err = iterateSSE(response.Body, func(sse serverSentEvent) error {
			if strings.TrimSpace(sse.Data) == "" || strings.TrimSpace(sse.Data) == "[DONE]" {
				return nil
			}
			var chunk map[string]any
			if err := json.Unmarshal([]byte(sse.Data), &chunk); err != nil {
				return fmt.Errorf("could not parse Google SSE event: %w; data=%s; raw=%s", err, sse.Data, strings.Join(sse.Raw, "\n"))
			}
			state.apply(stream, &output, model, chunk)
			return nil
		})
		if err != nil {
			pushFinalError(stream, &output, err.Error())
			return
		}
		finishGoogleCurrentBlock(stream, &output, &state.currentBlockIndex)
		stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
	}()
	return stream
}

func BuildGoogleParams(model ai.Model, llmContext ai.Context, options GoogleOptions) map[string]any {
	params := map[string]any{"contents": ConvertGoogleMessages(model, llmContext)}
	config := map[string]any{}
	if options.Temperature != nil {
		config["temperature"] = *options.Temperature
	}
	if options.MaxTokens != nil {
		config["maxOutputTokens"] = *options.MaxTokens
	}
	if modalities := googleResponseModalities(model); len(modalities) > 0 {
		config["responseModalities"] = modalities
	}
	if options.Thinking != nil && model.Reasoning {
		if options.Thinking.Enabled {
			thinking := map[string]any{"includeThoughts": true}
			if options.Thinking.Level != "" {
				thinking["thinkingLevel"] = options.Thinking.Level
			} else if options.Thinking.BudgetTokens != nil {
				thinking["thinkingBudget"] = *options.Thinking.BudgetTokens
			}
			config["thinkingConfig"] = thinking
		} else {
			config["thinkingConfig"] = disabledGoogleThinkingConfig(model)
		}
	}
	if len(config) > 0 {
		params["generationConfig"] = config
	}
	if llmContext.SystemPrompt != "" {
		params["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": aiutils.SanitizeSurrogates(llmContext.SystemPrompt)}}}
	}
	if len(llmContext.Tools) > 0 {
		params["tools"] = ConvertGoogleTools(llmContext.Tools, false)
		if options.ToolChoice != "" {
			params["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": mapGoogleToolChoice(options.ToolChoice)}}
		}
	}
	return params
}

func doGoogleRequest(ctx context.Context, model ai.Model, options GoogleOptions, params map[string]any) (*http.Response, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	endpoint, err := googleEndpoint(model)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Goog-Api-Key", options.APIKey)
	if isBeeperAIProxyBaseURL(model.BaseURL) {
		req.Header.Set("Authorization", "Bearer "+options.APIKey)
		req.Header.Del("X-Goog-Api-Key")
	}
	for key, value := range model.Headers {
		req.Header.Set(key, value)
	}
	for key, value := range options.Headers {
		req.Header.Set(key, value)
	}
	client := http.DefaultClient
	if options.TimeoutMs != nil && *options.TimeoutMs > 0 {
		client = &http.Client{Timeout: time.Duration(*options.TimeoutMs) * time.Millisecond}
	}
	client = aiutils.WithAIServicesLogging(client)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("Google API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return resp, nil
}

func googleEndpoint(model ai.Model) (string, error) {
	baseURL := strings.TrimRight(model.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	modelPath, err := googleModelPath(model.ID)
	if err != nil {
		return "", err
	}
	return baseURL + "/" + modelPath + ":streamGenerateContent?alt=sse", nil
}

func googleModelPath(modelID string) (string, error) {
	if modelID == "" || strings.Contains(modelID, "..") || strings.ContainsAny(modelID, "?&") {
		return "", fmt.Errorf("invalid model parameter")
	}
	modelID = strings.TrimPrefix(modelID, "google/")
	if strings.HasPrefix(modelID, "models/") {
		return modelID, nil
	}
	return "models/" + modelID, nil
}

func isGoogleGemma4Model(model ai.Model) bool {
	return regexpGemma4.MatchString(strings.ToLower(model.ID))
}

var regexpGemma4 = regexp.MustCompile(`gemma-?4`)

func disabledGoogleThinkingConfig(model ai.Model) map[string]any {
	if isGoogleGemini3ProModel(model) {
		return map[string]any{"thinkingLevel": googleThinkingLow}
	}
	if isGoogleGemini3FlashModel(model) || isGoogleGemma4Model(model) {
		return map[string]any{"thinkingLevel": googleThinkingMinimal}
	}
	return map[string]any{"thinkingBudget": 0}
}

func getGoogleThinkingLevel(effort ai.ThinkingLevel, model ai.Model) googleThinkingLevel {
	if isGoogleGemini3ProModel(model) {
		switch effort {
		case ai.ThinkingLevelMinimal, ai.ThinkingLevelLow:
			return googleThinkingLow
		case ai.ThinkingLevelMedium, ai.ThinkingLevelHigh:
			return googleThinkingHigh
		}
	}
	if isGoogleGemma4Model(model) {
		switch effort {
		case ai.ThinkingLevelMinimal, ai.ThinkingLevelLow:
			return googleThinkingMinimal
		case ai.ThinkingLevelMedium, ai.ThinkingLevelHigh:
			return googleThinkingHigh
		}
	}
	switch effort {
	case ai.ThinkingLevelMinimal:
		return googleThinkingMinimal
	case ai.ThinkingLevelLow:
		return googleThinkingLow
	case ai.ThinkingLevelMedium:
		return googleThinkingMedium
	case ai.ThinkingLevelHigh:
		return googleThinkingHigh
	default:
		return googleThinkingHigh
	}
}

func getGoogleBudget(model ai.Model, effort ai.ThinkingLevel, customBudgets *ai.ThinkingBudgets) int {
	if customBudgets != nil {
		switch effort {
		case ai.ThinkingLevelMinimal:
			if customBudgets.Minimal != nil {
				return *customBudgets.Minimal
			}
		case ai.ThinkingLevelLow:
			if customBudgets.Low != nil {
				return *customBudgets.Low
			}
		case ai.ThinkingLevelMedium:
			if customBudgets.Medium != nil {
				return *customBudgets.Medium
			}
		case ai.ThinkingLevelHigh:
			if customBudgets.High != nil {
				return *customBudgets.High
			}
		}
	}
	if strings.Contains(model.ID, "2.5-pro") {
		return map[ai.ThinkingLevel]int{ai.ThinkingLevelMinimal: 128, ai.ThinkingLevelLow: 2048, ai.ThinkingLevelMedium: 8192, ai.ThinkingLevelHigh: 32768}[effort]
	}
	if strings.Contains(model.ID, "2.5-flash-lite") {
		return map[ai.ThinkingLevel]int{ai.ThinkingLevelMinimal: 512, ai.ThinkingLevelLow: 2048, ai.ThinkingLevelMedium: 8192, ai.ThinkingLevelHigh: 24576}[effort]
	}
	if strings.Contains(model.ID, "2.5-flash") {
		return map[ai.ThinkingLevel]int{ai.ThinkingLevelMinimal: 128, ai.ThinkingLevelLow: 2048, ai.ThinkingLevelMedium: 8192, ai.ThinkingLevelHigh: 24576}[effort]
	}
	return -1
}
