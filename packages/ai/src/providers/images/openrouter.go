package images

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
	aiutils "github.com/earendil-works/pi-mono/packages/ai/src/utils"
)

var imageDataURLPattern = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

func GenerateImagesOpenRouter(ctx context.Context, model ai.ImagesModel, imageContext ai.ImagesContext, options ai.ImagesOptions) ai.AssistantImages {
	output := ai.AssistantImages{
		API:        model.API,
		Provider:   model.Provider,
		Model:      model.ID,
		Output:     []ai.ContentBlock{},
		StopReason: ai.ImagesStopReasonStop,
		Timestamp:  time.Now().UnixMilli(),
	}

	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = ai.GetEnvAPIKey(ai.Provider(model.Provider))
	}
	if apiKey == "" {
		return imageError(output, "No API key available for provider: "+string(model.Provider), ctx)
	}

	params := buildOpenRouterImagesParams(model, imageContext)
	if options.OnPayload != nil {
		next, ok, err := options.OnPayload(params, model)
		if err != nil {
			return imageError(output, err.Error(), ctx)
		}
		if ok {
			nextParams, ok := next.(map[string]any)
			if !ok {
				return imageError(output, "onPayload returned unsupported OpenAI request body", ctx)
			}
			params = nextParams
		}
	}

	var rawResponse *http.Response
	requestOptions := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(model.BaseURL),
		option.WithResponseInto(&rawResponse),
		option.WithRequestBody("application/json", params),
	}
	for key, value := range model.Headers {
		requestOptions = append(requestOptions, option.WithHeader(key, value))
	}
	for key, value := range options.Headers {
		requestOptions = append(requestOptions, option.WithHeader(key, value))
	}
	if options.TimeoutMs != nil {
		requestOptions = append(requestOptions, option.WithRequestTimeout(time.Duration(*options.TimeoutMs)*time.Millisecond))
	}
	if options.MaxRetries != nil {
		requestOptions = append(requestOptions, option.WithMaxRetries(*options.MaxRetries))
	}

	client := openaisdk.NewClient()
	response, err := client.Chat.Completions.New(ctx, openaisdk.ChatCompletionNewParams{
		Model: shared.ChatModel(model.ID),
		Messages: []openaisdk.ChatCompletionMessageParamUnion{{
			OfUser: &openaisdk.ChatCompletionUserMessageParam{
				Content: openaisdk.ChatCompletionUserMessageParamContentUnion{OfString: openaisdk.String("")},
			},
		}},
	}, requestOptions...)
	if err != nil {
		return imageError(output, formatOpenRouterImageError(err), ctx)
	}
	if options.OnResponse != nil && rawResponse != nil {
		if err := options.OnResponse(ai.ProviderResponse{Status: rawResponse.StatusCode, Headers: aiutils.HeadersToRecord(rawResponse.Header)}, model); err != nil {
			return imageError(output, err.Error(), ctx)
		}
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(response.RawJSON()), &raw); err != nil {
		return imageError(output, err.Error(), ctx)
	}
	if id, ok := raw["id"].(string); ok {
		output.ResponseID = id
	}
	if usage, ok := raw["usage"].(map[string]any); ok {
		output.Usage = parseOpenRouterImagesUsage(usage, model)
	}
	parseOpenRouterImagesChoice(&output, raw)
	return output
}

func buildOpenRouterImagesParams(model ai.ImagesModel, imageContext ai.ImagesContext) map[string]any {
	content := make([]map[string]any, 0, len(imageContext.Input))
	for _, item := range imageContext.Input {
		if item.Type == "image" {
			content = append(content, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": "data:" + item.MimeType + ";base64," + item.Data,
				},
			})
			continue
		}
		content = append(content, map[string]any{"type": "text", "text": aiutils.SanitizeSurrogates(item.Text)})
	}
	modalities := []string{"image"}
	for _, output := range model.Output {
		if output == "text" {
			modalities = []string{"image", "text"}
			break
		}
	}
	return map[string]any{
		"model": model.ID,
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"stream":     false,
		"modalities": modalities,
	}
}

func parseOpenRouterImagesChoice(output *ai.AssistantImages, raw map[string]any) {
	choices, ok := raw["choices"].([]any)
	if !ok || len(choices) == 0 {
		return
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return
	}
	if content, ok := message["content"].(string); ok && content != "" {
		output.Output = append(output.Output, ai.ContentBlock{Type: "text", Text: content})
	}
	images, ok := message["images"].([]any)
	if !ok {
		return
	}
	for _, rawImage := range images {
		image, ok := rawImage.(map[string]any)
		if !ok {
			continue
		}
		imageURL := imageURLString(image["image_url"])
		if !strings.HasPrefix(imageURL, "data:") {
			continue
		}
		matches := imageDataURLPattern.FindStringSubmatch(imageURL)
		if len(matches) != 3 {
			continue
		}
		output.Output = append(output.Output, ai.ContentBlock{Type: "image", MimeType: matches[1], Data: matches[2]})
	}
}

func imageURLString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		url, _ := typed["url"].(string)
		return url
	default:
		return ""
	}
}

func parseOpenRouterImagesUsage(rawUsage map[string]any, model ai.ImagesModel) ai.Usage {
	promptTokens := intFromAny(rawUsage["prompt_tokens"])
	completionTokens := intFromAny(rawUsage["completion_tokens"])
	details, _ := rawUsage["prompt_tokens_details"].(map[string]any)
	reportedCachedTokens := intFromAny(details["cached_tokens"])
	cacheWriteTokens := intFromAny(details["cache_write_tokens"])
	cacheReadTokens := reportedCachedTokens
	if cacheWriteTokens > 0 {
		cacheReadTokens = max(0, reportedCachedTokens-cacheWriteTokens)
	}
	input := max(0, promptTokens-cacheReadTokens-cacheWriteTokens)
	usage := ai.Usage{
		Input:       input,
		Output:      completionTokens,
		CacheRead:   cacheReadTokens,
		CacheWrite:  cacheWriteTokens,
		TotalTokens: input + completionTokens + cacheReadTokens + cacheWriteTokens,
		Cost: ai.UsageCost{
			Input:      (model.Cost.Input / 1000000) * float64(input),
			Output:     (model.Cost.Output / 1000000) * float64(completionTokens),
			CacheRead:  (model.Cost.CacheRead / 1000000) * float64(cacheReadTokens),
			CacheWrite: (model.Cost.CacheWrite / 1000000) * float64(cacheWriteTokens),
		},
	}
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
	return usage
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func imageError(output ai.AssistantImages, message string, ctx context.Context) ai.AssistantImages {
	if ctx.Err() != nil {
		output.StopReason = ai.ImagesStopReasonAborted
	} else {
		output.StopReason = ai.ImagesStopReasonError
	}
	output.ErrorMessage = message
	return output
}

func formatOpenRouterImageError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
