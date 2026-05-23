package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

type ProxyAssistantMessageEvent struct {
	Type             string        `json:"type"`
	ContentIndex     int           `json:"contentIndex,omitempty"`
	Delta            string        `json:"delta,omitempty"`
	ContentSignature string        `json:"contentSignature,omitempty"`
	ID               string        `json:"id,omitempty"`
	ToolName         string        `json:"toolName,omitempty"`
	Reason           ai.StopReason `json:"reason,omitempty"`
	Usage            ai.Usage      `json:"usage,omitempty"`
	ErrorMessage     string        `json:"errorMessage,omitempty"`
}

type ProxyStreamOptions struct {
	ai.SimpleStreamOptions
	AuthToken string
	ProxyURL  string
	Client    *http.Client
}

func StreamProxy(ctx context.Context, model ai.Model, llmContext ai.Context, options ProxyStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		partial := ai.Message{
			Role:       "assistant",
			StopReason: ai.StopReasonStop,
			Content:    []ai.ContentBlock{},
			API:        model.API,
			Provider:   model.Provider,
			Model:      model.ID,
			Usage:      ai.EmptyUsage(),
			Timestamp:  time.Now().UnixMilli(),
		}
		state := proxyStreamState{partialJSONByIndex: map[int]string{}}
		if err := streamProxyEvents(ctx, model, llmContext, options, stream, &partial, &state); err != nil {
			reason := ai.StopReasonError
			if ctx.Err() != nil {
				reason = ai.StopReasonAborted
			}
			partial.StopReason = reason
			partial.ErrorMessage = err.Error()
			stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: reason, Error: &partial})
		}
	}()
	return stream
}

func streamProxyEvents(ctx context.Context, model ai.Model, llmContext ai.Context, options ProxyStreamOptions, stream *ai.AssistantMessageEventStream, partial *ai.Message, state *proxyStreamState) error {
	body, err := json.Marshal(map[string]any{
		"model":   model,
		"context": llmContext,
		"options": buildProxyRequestOptions(options),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(options.ProxyURL, "/")+"/api/stream", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+options.AuthToken)
	req.Header.Set("Content-Type", "application/json")
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return proxyHTTPError(resp)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if data != "" {
				var proxyEvent ProxyAssistantMessageEvent
				if err := json.Unmarshal([]byte(data), &proxyEvent); err != nil {
					return err
				}
				event, ok, err := state.process(proxyEvent, partial)
				if err != nil {
					return err
				}
				if ok {
					stream.Push(event)
				}
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

func buildProxyRequestOptions(options ProxyStreamOptions) map[string]any {
	out := map[string]any{}
	if options.Temperature != nil {
		out["temperature"] = *options.Temperature
	}
	if options.MaxTokens != nil {
		out["maxTokens"] = *options.MaxTokens
	}
	if options.Reasoning != nil {
		out["reasoning"] = *options.Reasoning
	}
	if options.CacheRetention != "" {
		out["cacheRetention"] = options.CacheRetention
	}
	if options.SessionID != "" {
		out["sessionId"] = options.SessionID
	}
	if options.Headers != nil {
		out["headers"] = options.Headers
	}
	if options.Metadata != nil {
		out["metadata"] = options.Metadata
	}
	if options.Transport != "" {
		out["transport"] = options.Transport
	}
	if options.ThinkingBudgets != nil {
		out["thinkingBudgets"] = options.ThinkingBudgets
	}
	if options.MaxRetryDelayMs != nil {
		out["maxRetryDelayMs"] = *options.MaxRetryDelayMs
	}
	return out
}

type proxyStreamState struct {
	partialJSONByIndex map[int]string
}

func (s *proxyStreamState) process(proxyEvent ProxyAssistantMessageEvent, partial *ai.Message) (ai.AssistantMessageEvent, bool, error) {
	switch proxyEvent.Type {
	case "start":
		return ai.AssistantMessageEvent{Type: "start", Partial: partial}, true, nil
	case "text_start":
		setProxyBlock(partial, proxyEvent.ContentIndex, ai.ContentBlock{Type: "text"})
		return ai.AssistantMessageEvent{Type: "text_start", ContentIndex: proxyEvent.ContentIndex, Partial: partial}, true, nil
	case "text_delta":
		block, ok := proxyBlock(partial, proxyEvent.ContentIndex, "text")
		if !ok {
			return ai.AssistantMessageEvent{}, false, fmt.Errorf("Received text_delta for non-text content")
		}
		block.Text += proxyEvent.Delta
		setProxyBlock(partial, proxyEvent.ContentIndex, block)
		return ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: proxyEvent.ContentIndex, Delta: proxyEvent.Delta, Partial: partial}, true, nil
	case "text_end":
		block, ok := proxyBlock(partial, proxyEvent.ContentIndex, "text")
		if !ok {
			return ai.AssistantMessageEvent{}, false, fmt.Errorf("Received text_end for non-text content")
		}
		block.TextSignature = proxyEvent.ContentSignature
		setProxyBlock(partial, proxyEvent.ContentIndex, block)
		return ai.AssistantMessageEvent{Type: "text_end", ContentIndex: proxyEvent.ContentIndex, Content: block.Text, Partial: partial}, true, nil
	case "thinking_start":
		setProxyBlock(partial, proxyEvent.ContentIndex, ai.ContentBlock{Type: "thinking"})
		return ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: proxyEvent.ContentIndex, Partial: partial}, true, nil
	case "thinking_delta":
		block, ok := proxyBlock(partial, proxyEvent.ContentIndex, "thinking")
		if !ok {
			return ai.AssistantMessageEvent{}, false, fmt.Errorf("Received thinking_delta for non-thinking content")
		}
		block.Thinking += proxyEvent.Delta
		setProxyBlock(partial, proxyEvent.ContentIndex, block)
		return ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: proxyEvent.ContentIndex, Delta: proxyEvent.Delta, Partial: partial}, true, nil
	case "thinking_end":
		block, ok := proxyBlock(partial, proxyEvent.ContentIndex, "thinking")
		if !ok {
			return ai.AssistantMessageEvent{}, false, fmt.Errorf("Received thinking_end for non-thinking content")
		}
		block.ThinkingSignature = proxyEvent.ContentSignature
		setProxyBlock(partial, proxyEvent.ContentIndex, block)
		return ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: proxyEvent.ContentIndex, Content: block.Thinking, Partial: partial}, true, nil
	case "toolcall_start":
		setProxyBlock(partial, proxyEvent.ContentIndex, ai.ContentBlock{Type: "toolCall", ID: proxyEvent.ID, Name: proxyEvent.ToolName, Arguments: map[string]any{}})
		s.partialJSONByIndex[proxyEvent.ContentIndex] = ""
		return ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: proxyEvent.ContentIndex, Partial: partial}, true, nil
	case "toolcall_delta":
		block, ok := proxyBlock(partial, proxyEvent.ContentIndex, "toolCall")
		if !ok {
			return ai.AssistantMessageEvent{}, false, fmt.Errorf("Received toolcall_delta for non-toolCall content")
		}
		s.partialJSONByIndex[proxyEvent.ContentIndex] += proxyEvent.Delta
		block.Arguments = parseProxyStreamingJSON(s.partialJSONByIndex[proxyEvent.ContentIndex])
		setProxyBlock(partial, proxyEvent.ContentIndex, block)
		return ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: proxyEvent.ContentIndex, Delta: proxyEvent.Delta, Partial: partial}, true, nil
	case "toolcall_end":
		block, ok := proxyBlock(partial, proxyEvent.ContentIndex, "toolCall")
		if !ok {
			return ai.AssistantMessageEvent{}, false, nil
		}
		delete(s.partialJSONByIndex, proxyEvent.ContentIndex)
		toolCall := ai.ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: block.Arguments}
		return ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: proxyEvent.ContentIndex, ToolCall: &toolCall, Partial: partial}, true, nil
	case "done":
		partial.StopReason = proxyEvent.Reason
		partial.Usage = proxyEvent.Usage
		return ai.AssistantMessageEvent{Type: "done", Reason: proxyEvent.Reason, Message: partial}, true, nil
	case "error":
		partial.StopReason = proxyEvent.Reason
		partial.ErrorMessage = proxyEvent.ErrorMessage
		partial.Usage = proxyEvent.Usage
		return ai.AssistantMessageEvent{Type: "error", Reason: proxyEvent.Reason, Error: partial}, true, nil
	default:
		return ai.AssistantMessageEvent{}, false, nil
	}
}

func setProxyBlock(partial *ai.Message, index int, block ai.ContentBlock) {
	blocks := proxyContent(partial)
	for len(blocks) <= index {
		blocks = append(blocks, ai.ContentBlock{})
	}
	blocks[index] = block
	partial.Content = blocks
}

func proxyBlock(partial *ai.Message, index int, blockType string) (ai.ContentBlock, bool) {
	blocks := proxyContent(partial)
	if index < 0 || index >= len(blocks) || blocks[index].Type != blockType {
		return ai.ContentBlock{}, false
	}
	return blocks[index], true
}

func proxyContent(partial *ai.Message) []ai.ContentBlock {
	if blocks, ok := partial.Content.([]ai.ContentBlock); ok {
		return blocks
	}
	raw, _ := json.Marshal(partial.Content)
	var blocks []ai.ContentBlock
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

func parseProxyStreamingJSON(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(raw), &parsed) == nil {
		return parsed
	}
	for suffix := 1; suffix <= 2; suffix++ {
		candidate := raw + strings.Repeat("}", suffix)
		if json.Unmarshal([]byte(candidate), &parsed) == nil {
			return parsed
		}
	}
	return map[string]any{}
}

func proxyHTTPError(resp *http.Response) error {
	message := fmt.Sprintf("Proxy error: %d %s", resp.StatusCode, resp.Status)
	var payload struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) == nil && payload.Error != "" {
		message = "Proxy error: " + payload.Error
	}
	return errors.New(message)
}
