package aitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

const (
	DefaultAPI          ai.Api      = "faux"
	DefaultProvider     ai.Provider = "faux"
	DefaultModelID                  = "faux-1"
	DefaultModelName                = "Faux Model"
	DefaultBaseURL                  = "http://localhost:0"
	DefaultMinTokenSize             = 3
	DefaultMaxTokenSize             = 5
)

type FauxModelDefinition struct {
	ID            string
	Name          string
	Reasoning     bool
	Input         []string
	Cost          ai.ModelCost
	ContextWindow int
	MaxTokens     int
}

type RegisterFauxProviderOptions struct {
	API             ai.Api
	Provider        ai.Provider
	Models          []FauxModelDefinition
	TokensPerSecond float64
	TokenSize       TokenSize
}

type TokenSize struct {
	Min int
	Max int
}

type ResponseState struct {
	CallCount int
}

type ResponseFactory func(ctx context.Context, llmContext ai.Context, options ai.SimpleStreamOptions, state ResponseState, model ai.Model) (ai.Message, error)

type ResponseStep struct {
	factory ResponseFactory
}

type Registration struct {
	API      ai.Api
	Provider ai.Provider
	Models   []ai.Model
	State    ResponseState

	sourceID        string
	minTokenSize    int
	maxTokenSize    int
	tokensPerSecond float64
	pending         []ResponseStep
	promptCache     map[string]string
	mu              sync.Mutex
}

type MessageOption func(*ai.Message)

func Text(text string) ai.ContentBlock {
	return ai.ContentBlock{Type: "text", Text: text}
}

func Thinking(thinking string) ai.ContentBlock {
	return ai.ContentBlock{Type: "thinking", Thinking: thinking}
}

func ToolCall(name string, args map[string]any, id ...string) ai.ContentBlock {
	toolID := ""
	if len(id) > 0 {
		toolID = id[0]
	}
	if toolID == "" {
		toolID = randomID("tool")
	}
	return ai.ContentBlock{Type: "toolCall", ID: toolID, Name: name, Arguments: cloneAnyMap(args)}
}

func WithStopReason(reason ai.StopReason) MessageOption {
	return func(message *ai.Message) {
		message.StopReason = reason
	}
}

func WithError(message string) MessageOption {
	return func(aiMessage *ai.Message) {
		aiMessage.StopReason = ai.StopReasonError
		aiMessage.ErrorMessage = message
	}
}

func WithResponseID(responseID string) MessageOption {
	return func(message *ai.Message) {
		message.ResponseID = responseID
	}
}

func WithTimestamp(timestamp int64) MessageOption {
	return func(message *ai.Message) {
		message.Timestamp = timestamp
	}
}

func AssistantText(text string, options ...MessageOption) ai.Message {
	return AssistantBlocks([]ai.ContentBlock{Text(text)}, options...)
}

func AssistantBlocks(blocks []ai.ContentBlock, options ...MessageOption) ai.Message {
	message := ai.Message{
		Role:       "assistant",
		Content:    cloneBlocks(blocks),
		API:        DefaultAPI,
		Provider:   DefaultProvider,
		Model:      DefaultModelID,
		Usage:      ai.EmptyUsage(),
		StopReason: ai.StopReasonStop,
		Timestamp:  time.Now().UnixMilli(),
	}
	for _, option := range options {
		option(&message)
	}
	return message
}

func Static(message ai.Message) ResponseStep {
	return ResponseStep{factory: func(context.Context, ai.Context, ai.SimpleStreamOptions, ResponseState, ai.Model) (ai.Message, error) {
		return message, nil
	}}
}

func Factory(factory ResponseFactory) ResponseStep {
	return ResponseStep{factory: factory}
}

func RegisterFauxProvider(options RegisterFauxProviderOptions) *Registration {
	api := options.API
	if api == "" {
		api = ai.Api(randomID(string(DefaultAPI)))
	}
	provider := options.Provider
	if provider == "" {
		provider = DefaultProvider
	}
	minTokenSize := options.TokenSize.Min
	if minTokenSize <= 0 {
		minTokenSize = DefaultMinTokenSize
	}
	maxTokenSize := options.TokenSize.Max
	if maxTokenSize <= 0 {
		maxTokenSize = DefaultMaxTokenSize
	}
	if minTokenSize > maxTokenSize {
		minTokenSize = maxTokenSize
	}
	if minTokenSize <= 0 {
		minTokenSize = 1
	}
	modelDefinitions := options.Models
	if len(modelDefinitions) == 0 {
		modelDefinitions = []FauxModelDefinition{{
			ID:            DefaultModelID,
			Name:          DefaultModelName,
			Input:         []string{"text", "image"},
			ContextWindow: 128000,
			MaxTokens:     16384,
		}}
	}
	models := make([]ai.Model, 0, len(modelDefinitions))
	for _, definition := range modelDefinitions {
		id := definition.ID
		if id == "" {
			id = DefaultModelID
		}
		name := definition.Name
		if name == "" {
			name = id
		}
		input := append([]string{}, definition.Input...)
		if len(input) == 0 {
			input = []string{"text", "image"}
		}
		contextWindow := definition.ContextWindow
		if contextWindow == 0 {
			contextWindow = 128000
		}
		maxTokens := definition.MaxTokens
		if maxTokens == 0 {
			maxTokens = 16384
		}
		models = append(models, ai.Model{
			ID:            id,
			Name:          name,
			API:           api,
			Provider:      provider,
			BaseURL:       DefaultBaseURL,
			Reasoning:     definition.Reasoning,
			Input:         input,
			Cost:          definition.Cost,
			ContextWindow: contextWindow,
			MaxTokens:     maxTokens,
		})
	}
	registration := &Registration{
		API:             api,
		Provider:        provider,
		Models:          models,
		sourceID:        randomID("faux-provider"),
		minTokenSize:    minTokenSize,
		maxTokenSize:    maxTokenSize,
		tokensPerSecond: options.TokensPerSecond,
		promptCache:     map[string]string{},
	}
	ai.RegisterAPIProviderWithSource(ai.APIProvider{
		API: api,
		Stream: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.StreamOptions) *ai.AssistantMessageEventStream {
			return registration.stream(ctx, model, llmContext, ai.SimpleStreamOptions{StreamOptions: options})
		},
		StreamSimple: registration.stream,
	}, registration.sourceID)
	return registration
}

func (r *Registration) Unregister() {
	ai.UnregisterAPIProviders(r.sourceID)
}

func (r *Registration) GetModel(modelID ...string) (ai.Model, bool) {
	if len(modelID) == 0 || modelID[0] == "" {
		if len(r.Models) == 0 {
			return ai.Model{}, false
		}
		return r.Models[0], true
	}
	for _, model := range r.Models {
		if model.ID == modelID[0] {
			return model, true
		}
	}
	return ai.Model{}, false
}

func (r *Registration) MustModel(modelID ...string) ai.Model {
	model, ok := r.GetModel(modelID...)
	if !ok {
		if len(modelID) == 0 {
			panic("faux model not found")
		}
		panic("faux model not found: " + modelID[0])
	}
	return model
}

func (r *Registration) SetResponses(responses ...ResponseStep) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append([]ResponseStep{}, responses...)
}

func (r *Registration) SetMessages(messages ...ai.Message) {
	steps := make([]ResponseStep, 0, len(messages))
	for _, message := range messages {
		steps = append(steps, Static(message))
	}
	r.SetResponses(steps...)
}

func (r *Registration) AppendResponses(responses ...ResponseStep) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, responses...)
}

func (r *Registration) AppendMessages(messages ...ai.Message) {
	steps := make([]ResponseStep, 0, len(messages))
	for _, message := range messages {
		steps = append(steps, Static(message))
	}
	r.AppendResponses(steps...)
}

func (r *Registration) PendingResponseCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

func (r *Registration) stream(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	step, state := r.nextResponse()
	go func() {
		if options.OnResponse != nil {
			if err := options.OnResponse(ai.ProviderResponse{Status: 200, Headers: map[string]string{}}, model); err != nil {
				pushError(stream, model, err)
				return
			}
		}
		if step.factory == nil {
			message := r.withUsageEstimate(errorMessage(model, errors.New("No more faux responses queued")), llmContext, options)
			stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonError, Error: &message})
			return
		}
		message, err := step.factory(ctx, llmContext, options, state, model)
		if err != nil {
			pushError(stream, model, err)
			return
		}
		message = r.withUsageEstimate(cloneMessage(message, r.API, r.Provider, model.ID), llmContext, options)
		streamWithDeltas(ctx, stream, message, r.minTokenSize, r.maxTokenSize, r.tokensPerSecond)
	}()
	return stream
}

func (r *Registration) nextResponse() (ResponseStep, ResponseState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var step ResponseStep
	if len(r.pending) > 0 {
		step = r.pending[0]
		r.pending = r.pending[1:]
	}
	r.State.CallCount++
	return step, r.State
}

func (r *Registration) withUsageEstimate(message ai.Message, llmContext ai.Context, options ai.SimpleStreamOptions) ai.Message {
	promptText := serializeContext(llmContext)
	promptTokens := estimateTokens(promptText)
	outputTokens := estimateTokens(blocksToText(contentBlocks(message.Content)))
	input := promptTokens
	cacheRead := 0
	cacheWrite := 0
	if options.SessionID != "" && options.CacheRetention != ai.CacheRetentionNone {
		r.mu.Lock()
		previousPrompt := r.promptCache[options.SessionID]
		if previousPrompt == "" {
			cacheWrite = promptTokens
		} else {
			commonPrefix := commonPrefixLength(previousPrompt, promptText)
			cacheRead = estimateTokens(previousPrompt[:commonPrefix])
			cacheWrite = estimateTokens(promptText[commonPrefix:])
			input = max(0, promptTokens-cacheRead)
		}
		r.promptCache[options.SessionID] = promptText
		r.mu.Unlock()
	}
	message.Usage = ai.Usage{
		Input:       input,
		Output:      outputTokens,
		CacheRead:   cacheRead,
		CacheWrite:  cacheWrite,
		TotalTokens: input + outputTokens + cacheRead + cacheWrite,
		Cost:        ai.UsageCost{},
	}
	return message
}

func streamWithDeltas(ctx context.Context, stream *ai.AssistantMessageEventStream, message ai.Message, minTokenSize int, maxTokenSize int, tokensPerSecond float64) {
	partial := message
	partial.Content = []ai.ContentBlock{}
	if ctx.Err() != nil {
		aborted := abortedMessage(partial)
		stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonAborted, Error: &aborted})
		return
	}
	stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &partial})
	for index, block := range contentBlocks(message.Content) {
		if ctx.Err() != nil {
			aborted := abortedMessage(partial)
			stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonAborted, Error: &aborted})
			return
		}
		switch block.Type {
		case "thinking":
			partialBlocks := contentBlocks(partial.Content)
			partialBlocks = append(partialBlocks, ai.ContentBlock{Type: "thinking"})
			partial.Content = partialBlocks
			stream.Push(ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: index, Partial: &partial})
			for _, chunk := range splitStringByTokenSize(block.Thinking, minTokenSize, maxTokenSize) {
				if !sleepChunk(ctx, chunk, tokensPerSecond) {
					aborted := abortedMessage(partial)
					stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonAborted, Error: &aborted})
					return
				}
				partialBlocks := contentBlocks(partial.Content)
				partialBlocks[index].Thinking += chunk
				partial.Content = partialBlocks
				stream.Push(ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: index, Delta: chunk, Partial: &partial})
			}
			stream.Push(ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: index, Content: block.Thinking, Partial: &partial})
		case "text":
			partialBlocks := contentBlocks(partial.Content)
			partialBlocks = append(partialBlocks, ai.ContentBlock{Type: "text"})
			partial.Content = partialBlocks
			stream.Push(ai.AssistantMessageEvent{Type: "text_start", ContentIndex: index, Partial: &partial})
			for _, chunk := range splitStringByTokenSize(block.Text, minTokenSize, maxTokenSize) {
				if !sleepChunk(ctx, chunk, tokensPerSecond) {
					aborted := abortedMessage(partial)
					stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonAborted, Error: &aborted})
					return
				}
				partialBlocks := contentBlocks(partial.Content)
				partialBlocks[index].Text += chunk
				partial.Content = partialBlocks
				stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: index, Delta: chunk, Partial: &partial})
			}
			stream.Push(ai.AssistantMessageEvent{Type: "text_end", ContentIndex: index, Content: block.Text, Partial: &partial})
		case "toolCall":
			partialBlocks := contentBlocks(partial.Content)
			partialBlocks = append(partialBlocks, ai.ContentBlock{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: map[string]any{}})
			partial.Content = partialBlocks
			stream.Push(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: index, Partial: &partial})
			args := mustJSON(block.Arguments)
			for _, chunk := range splitStringByTokenSize(args, minTokenSize, maxTokenSize) {
				if !sleepChunk(ctx, chunk, tokensPerSecond) {
					aborted := abortedMessage(partial)
					stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonAborted, Error: &aborted})
					return
				}
				stream.Push(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: index, Delta: chunk, Partial: &partial})
			}
			partialBlocks = contentBlocks(partial.Content)
			partialBlocks[index].Arguments = cloneAnyMap(block.Arguments)
			partial.Content = partialBlocks
			toolCall := ai.ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: cloneAnyMap(block.Arguments)}
			stream.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: index, ToolCall: &toolCall, Partial: &partial})
		}
	}
	if message.StopReason == ai.StopReasonError || message.StopReason == ai.StopReasonAborted {
		stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: message.StopReason, Error: &message})
		return
	}
	stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
}

func sleepChunk(ctx context.Context, chunk string, tokensPerSecond float64) bool {
	if tokensPerSecond <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	delay := time.Duration(float64(estimateTokens(chunk)) / tokensPerSecond * float64(time.Second))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func splitStringByTokenSize(text string, minTokenSize int, maxTokenSize int) []string {
	if text == "" {
		return []string{""}
	}
	chunks := []string{}
	index := 0
	for index < len(text) {
		tokenSize := minTokenSize
		if maxTokenSize > minTokenSize {
			tokenSize += rand.Intn(maxTokenSize - minTokenSize + 1)
		}
		charSize := max(1, tokenSize*4)
		end := min(len(text), index+charSize)
		chunks = append(chunks, text[index:end])
		index = end
	}
	return chunks
}

func cloneMessage(message ai.Message, api ai.Api, provider ai.Provider, modelID string) ai.Message {
	message.API = api
	message.Provider = provider
	message.Model = modelID
	if message.Timestamp == 0 {
		message.Timestamp = time.Now().UnixMilli()
	}
	if message.Usage == (ai.Usage{}) {
		message.Usage = ai.EmptyUsage()
	}
	return message
}

func errorMessage(model ai.Model, err error) ai.Message {
	return ai.Message{
		Role:         "assistant",
		Content:      []ai.ContentBlock{},
		API:          model.API,
		Provider:     model.Provider,
		Model:        model.ID,
		Usage:        ai.EmptyUsage(),
		StopReason:   ai.StopReasonError,
		ErrorMessage: err.Error(),
		Timestamp:    time.Now().UnixMilli(),
	}
}

func abortedMessage(partial ai.Message) ai.Message {
	partial.StopReason = ai.StopReasonAborted
	partial.ErrorMessage = "Request was aborted"
	partial.Timestamp = time.Now().UnixMilli()
	return partial
}

func pushError(stream *ai.AssistantMessageEventStream, model ai.Model, err error) {
	message := errorMessage(model, err)
	stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonError, Error: &message})
}

func serializeContext(llmContext ai.Context) string {
	parts := []string{}
	if llmContext.SystemPrompt != "" {
		parts = append(parts, "system:"+llmContext.SystemPrompt)
	}
	for _, message := range llmContext.Messages {
		parts = append(parts, message.Role+":"+messageToText(message))
	}
	if len(llmContext.Tools) > 0 {
		parts = append(parts, "tools:"+mustJSON(llmContext.Tools))
	}
	return strings.Join(parts, "\n\n")
}

func messageToText(message ai.Message) string {
	switch message.Role {
	case "user":
		return contentToText(message.Content)
	case "assistant":
		return blocksToText(contentBlocks(message.Content))
	case "toolResult":
		return strings.Join([]string{message.ToolName, contentToText(message.Content)}, "\n")
	default:
		return contentToText(message.Content)
	}
}

func contentToText(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []ai.ContentBlock:
		return blocksToText(typed)
	case []map[string]any:
		parts := make([]string, 0, len(typed))
		for _, block := range typed {
			if blockType, _ := block["type"].(string); blockType == "text" {
				parts = append(parts, fmt.Sprint(block["text"]))
			} else {
				parts = append(parts, mustJSON(block))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return mustJSON(typed)
	}
}

func blocksToText(blocks []ai.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "thinking":
			parts = append(parts, block.Thinking)
		case "image":
			parts = append(parts, fmt.Sprintf("[image:%s:%d]", block.MimeType, len(block.Data)))
		case "toolCall":
			parts = append(parts, block.Name+":"+mustJSON(block.Arguments))
		default:
			parts = append(parts, mustJSON(block))
		}
	}
	return strings.Join(parts, "\n")
}

func contentBlocks(content any) []ai.ContentBlock {
	switch typed := content.(type) {
	case nil:
		return nil
	case []ai.ContentBlock:
		return cloneBlocks(typed)
	case ai.ContentBlock:
		return []ai.ContentBlock{typed}
	case string:
		return []ai.ContentBlock{{Type: "text", Text: typed}}
	case []map[string]any:
		raw, _ := json.Marshal(typed)
		var blocks []ai.ContentBlock
		_ = json.Unmarshal(raw, &blocks)
		return blocks
	case []any:
		raw, _ := json.Marshal(typed)
		var blocks []ai.ContentBlock
		_ = json.Unmarshal(raw, &blocks)
		return blocks
	default:
		raw, _ := json.Marshal(typed)
		var blocks []ai.ContentBlock
		_ = json.Unmarshal(raw, &blocks)
		return blocks
	}
}

func cloneBlocks(blocks []ai.ContentBlock) []ai.ContentBlock {
	out := make([]ai.ContentBlock, len(blocks))
	for i, block := range blocks {
		out[i] = block
		out[i].Arguments = cloneAnyMap(block.Arguments)
	}
	return out
}

func cloneAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

func commonPrefixLength(a string, b string) int {
	limit := min(len(a), len(b))
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func randomID(prefix string) string {
	return fmt.Sprintf("%s:%d:%d", prefix, time.Now().UnixNano(), rand.Int63())
}
