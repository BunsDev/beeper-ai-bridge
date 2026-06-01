package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type OpenAIResponsesOptions struct {
	ai.StreamOptions
	ReasoningEffort  *ai.ThinkingLevel
	ReasoningSummary *string
	ServiceTier      string
}

func StreamSimpleOpenAIResponses(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	if options.APIKey == "" {
		options.APIKey = getEnvAPIKey(model.Provider)
	}
	if options.APIKey == "" {
		stream := ai.NewAssistantMessageEventStream()
		go pushError(stream, model, "No API key for provider: "+string(model.Provider))
		return stream
	}
	return StreamOpenAIResponses(ctx, model, llmContext, OpenAIResponsesOptions{StreamOptions: options.StreamOptions, ReasoningEffort: simpleReasoningEffort(model, options.Reasoning)})
}

func CompleteSimpleOpenAIResponses(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) ai.Message {
	if options.APIKey == "" {
		options.APIKey = getEnvAPIKey(model.Provider)
	}
	if options.APIKey == "" {
		output := newAssistant(model)
		output.StopReason = ai.StopReasonError
		output.ErrorMessage = "No API key for provider: " + string(model.Provider)
		return output
	}
	return CompleteOpenAIResponses(ctx, model, llmContext, OpenAIResponsesOptions{StreamOptions: options.StreamOptions, ReasoningEffort: simpleReasoningEffort(model, options.Reasoning)})
}

func CompleteOpenAIResponses(ctx context.Context, model ai.Model, llmContext ai.Context, options OpenAIResponsesOptions) ai.Message {
	output := newAssistant(model)
	params := BuildResponsesParams(model, llmContext, options)
	params["stream"] = false
	if options.OnPayload != nil {
		if next, ok, err := options.OnPayload(params, model); err != nil {
			output.StopReason = ai.StopReasonError
			output.ErrorMessage = err.Error()
			return output
		} else if ok {
			nextParams, ok := next.(map[string]any)
			if !ok {
				output.StopReason = ai.StopReasonError
				output.ErrorMessage = "onPayload returned unsupported OpenAI request body"
				return output
			}
			params = nextParams
		}
	}
	var rawResponse *http.Response
	client, requestOptions, err := newClient(model, llmContext, options.StreamOptions)
	if err != nil {
		output.StopReason = ai.StopReasonError
		output.ErrorMessage = err.Error()
		return output
	}
	requestOptions = append(requestOptions, option.WithResponseInto(&rawResponse))
	response, err := client.Responses.New(ctx, param.Override[responses.ResponseNewParams](params), requestOptions...)
	if err != nil {
		output.StopReason = ai.StopReasonError
		output.ErrorMessage = formatOpenAIError(err)
		return output
	}
	if options.OnResponse != nil && rawResponse != nil {
		if err := options.OnResponse(providerResponse(rawResponse), model); err != nil {
			output.StopReason = ai.StopReasonError
			output.ErrorMessage = err.Error()
			return output
		}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(response.RawJSON()), &raw); err != nil {
		output.StopReason = ai.StopReasonError
		output.ErrorMessage = err.Error()
		return output
	}
	applyCompleteOpenAIResponses(&output, model, options, raw)
	return output
}

func StreamOpenAIResponses(ctx context.Context, model ai.Model, llmContext ai.Context, options OpenAIResponsesOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		output := newAssistant(model)
		params := BuildResponsesParams(model, llmContext, options)
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
		sdkStream := client.Responses.NewStreaming(ctx, param.Override[responses.ResponseNewParams](params), requestOptions...)
		defer sdkStream.Close()

		if options.OnResponse != nil && rawResponse != nil {
			if err := options.OnResponse(providerResponse(rawResponse), model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			}
		}
		stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
		state := newResponsesStreamState()
		for sdkStream.Next() {
			event := sdkStream.Current()
			raw := event.RawJSON()
			var decoded map[string]any
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				continue
			}
			state.apply(stream, &output, model, options, decoded)
		}
		if err := sdkStream.Err(); err != nil {
			pushFinalError(stream, &output, formatOpenAIError(err))
			return
		}
		finishResponsesStream(stream, &output, state)
	}()
	return stream
}

func applyCompleteOpenAIResponses(output *ai.Message, model ai.Model, options OpenAIResponsesOptions, raw map[string]any) {
	if id, ok := raw["id"].(string); ok {
		output.ResponseID = id
	}
	if responseModel, ok := raw["model"].(string); ok && responseModel != "" && responseModel != model.ID {
		output.ResponseModel = responseModel
	}
	if usage, ok := raw["usage"].(map[string]any); ok {
		output.Usage = parseResponsesUsageMap(usage, model)
	}
	status, _ := raw["status"].(string)
	output.StopReason = mapResponsesStopReason(status)
	blocks := []ai.ContentBlock{}
	if items, ok := raw["output"].([]any); ok {
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]any)
			switch itemType, _ := item["type"].(string); itemType {
			case "reasoning":
				thinking := reasoningTextFromItem(item, "")
				if thinking != "" {
					blocks = append(blocks, ai.ContentBlock{Type: "thinking", Thinking: thinking, ThinkingSignature: mustJSON(item)})
				}
			case "message":
				if text := messageTextFromItem(item); text != "" {
					block := ai.ContentBlock{Type: "text", Text: text}
					if id, ok := item["id"].(string); ok && id != "" {
						block.TextSignature = mustJSON(map[string]any{"v": 1, "id": id})
					}
					output.Citations = append(output.Citations, providerCitationsFromAny(item, model.Provider, len(blocks))...)
					blocks = append(blocks, block)
				}
			case "function_call":
				id := fmt.Sprintf("%v|%v", item["call_id"], item["id"])
				args, _ := item["arguments"].(string)
				blocks = append(blocks, ai.ContentBlock{Type: "toolCall", ID: id, Name: fmt.Sprint(item["name"]), Arguments: parseJSONMap(args)})
			case "image_generation_call":
				blocks = append(blocks, imageBlockFromGenerationItem(item, ai.ContentBlock{}))
			}
		}
	}
	output.Content = blocks
	for _, block := range blocks {
		if block.Type == "toolCall" && output.StopReason == ai.StopReasonStop {
			output.StopReason = ai.StopReasonToolUse
			break
		}
	}
	serviceTier := options.ServiceTier
	if responseTier, ok := raw["service_tier"].(string); ok && responseTier != "" {
		serviceTier = responseTier
	}
	applyServiceTierPricing(&output.Usage, model, serviceTier)
	if output.StopReason == ai.StopReasonError {
		output.ErrorMessage = responseFailedMessage(map[string]any{"response": raw})
	}
}

func finishResponsesStream(stream *ai.AssistantMessageEventStream, output *ai.Message, state *responsesStreamState) {
	output.Content = state.blocks
	if output.StopReason == ai.StopReasonError || output.StopReason == ai.StopReasonAborted {
		stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: output.StopReason, Error: output})
		return
	}
	stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: output})
}

func BuildResponsesParams(model ai.Model, llmContext ai.Context, options OpenAIResponsesOptions) map[string]any {
	compat := ResolveOpenAIResponsesCompat(model)
	cacheRetention := resolveCacheRetention(options.CacheRetention)
	params := map[string]any{"model": model.ID, "input": ConvertResponsesMessages(model, llmContext), "stream": true, "store": false}
	if options.MaxTokens != nil {
		params["max_output_tokens"] = *options.MaxTokens
	}
	if options.Temperature != nil {
		params["temperature"] = *options.Temperature
	}
	if options.ServiceTier != "" {
		params["service_tier"] = options.ServiceTier
	}
	if len(llmContext.Tools) > 0 {
		params["tools"] = ConvertResponsesTools(llmContext.Tools)
	}
	if options.SessionID != "" && cacheRetention != ai.CacheRetentionNone {
		params["prompt_cache_key"] = clampPromptCacheKey(options.SessionID)
	}
	if cacheRetention == ai.CacheRetentionLong && compat.SupportsLongCacheRetention {
		params["prompt_cache_retention"] = "24h"
	}
	if model.Reasoning {
		if options.ReasoningEffort != nil || options.ReasoningSummary != nil {
			effort := "medium"
			if options.ReasoningEffort != nil {
				effort = mappedThinkingLevel(model, *options.ReasoningEffort)
			}
			summary := "auto"
			if options.ReasoningSummary != nil {
				summary = *options.ReasoningSummary
			}
			params["reasoning"] = map[string]any{"effort": effort, "summary": summary}
			params["include"] = []string{"reasoning.encrypted_content"}
		} else if model.Provider != "github-copilot" {
			if off := mappedOffThinkingLevel(model); off != "" {
				params["reasoning"] = map[string]any{"effort": off}
			}
		}
	}
	return params
}
