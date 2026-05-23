package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
	aiutils "github.com/earendil-works/pi-mono/packages/ai/src/utils"
)

type AgentEventSink func(context.Context, AgentEvent) error

type AgentLoopEventStream struct {
	*aiutils.EventStream[AgentEvent, []AgentMessage]
	mu  sync.Mutex
	err error
}

func NewAgentLoopEventStream() *AgentLoopEventStream {
	return &AgentLoopEventStream{EventStream: aiutils.NewEventStream[AgentEvent, []AgentMessage]()}
}

func (s *AgentLoopEventStream) Error() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *AgentLoopEventStream) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func AgentLoop(ctx context.Context, prompts []AgentMessage, agentContext AgentContext, config AgentLoopConfig, streamFn ...StreamFn) *AgentLoopEventStream {
	stream := NewAgentLoopEventStream()
	go func() {
		messages, err := RunAgentLoop(ctx, prompts, agentContext, config, func(ctx context.Context, event AgentEvent) error {
			stream.Push(event)
			return nil
		}, optionalStreamFn(streamFn))
		stream.setError(err)
		stream.End(messages)
	}()
	return stream
}

func AgentLoopContinue(ctx context.Context, agentContext AgentContext, config AgentLoopConfig, streamFn ...StreamFn) *AgentLoopEventStream {
	stream := NewAgentLoopEventStream()
	go func() {
		messages, err := RunAgentLoopContinue(ctx, agentContext, config, func(ctx context.Context, event AgentEvent) error {
			stream.Push(event)
			return nil
		}, optionalStreamFn(streamFn))
		stream.setError(err)
		stream.End(messages)
	}()
	return stream
}

func optionalStreamFn(streamFn []StreamFn) StreamFn {
	if len(streamFn) == 0 {
		return nil
	}
	return streamFn[0]
}

func RunAgentLoop(ctx context.Context, prompts []AgentMessage, agentContext AgentContext, config AgentLoopConfig, emit AgentEventSink, streamFn StreamFn) ([]AgentMessage, error) {
	newMessages := append([]AgentMessage{}, prompts...)
	currentContext := agentContext
	currentContext.Messages = append(append([]AgentMessage{}, agentContext.Messages...), prompts...)
	if err := emit(ctx, AgentEvent{Type: "agent_start"}); err != nil {
		return newMessages, err
	}
	if err := emit(ctx, AgentEvent{Type: "turn_start"}); err != nil {
		return newMessages, err
	}
	for i := range prompts {
		message := prompts[i]
		if err := emit(ctx, AgentEvent{Type: "message_start", Message: &message}); err != nil {
			return newMessages, err
		}
		if err := emit(ctx, AgentEvent{Type: "message_end", Message: &message}); err != nil {
			return newMessages, err
		}
	}
	err := runLoop(ctx, &currentContext, &newMessages, config, emit, streamFn)
	return newMessages, err
}

func RunAgentLoopContinue(ctx context.Context, agentContext AgentContext, config AgentLoopConfig, emit AgentEventSink, streamFn StreamFn) ([]AgentMessage, error) {
	if len(agentContext.Messages) == 0 {
		return nil, errors.New("Cannot continue: no messages in context")
	}
	if agentContext.Messages[len(agentContext.Messages)-1].Role == "assistant" {
		return nil, errors.New("Cannot continue from message role: assistant")
	}
	newMessages := []AgentMessage{}
	currentContext := agentContext
	if err := emit(ctx, AgentEvent{Type: "agent_start"}); err != nil {
		return newMessages, err
	}
	if err := emit(ctx, AgentEvent{Type: "turn_start"}); err != nil {
		return newMessages, err
	}
	err := runLoop(ctx, &currentContext, &newMessages, config, emit, streamFn)
	return newMessages, err
}

func runLoop(ctx context.Context, currentContext *AgentContext, newMessages *[]AgentMessage, config AgentLoopConfig, emit AgentEventSink, streamFn StreamFn) error {
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	firstTurn := true
	pendingMessages, err := getMessages(ctx, config.GetSteeringMessages)
	if err != nil {
		return err
	}
	for {
		hasMoreToolCalls := true
		for hasMoreToolCalls || len(pendingMessages) > 0 {
			if firstTurn {
				firstTurn = false
			} else if err := emit(ctx, AgentEvent{Type: "turn_start"}); err != nil {
				return err
			}
			for _, message := range pendingMessages {
				if err := emit(ctx, AgentEvent{Type: "message_start", Message: &message}); err != nil {
					return err
				}
				if err := emit(ctx, AgentEvent{Type: "message_end", Message: &message}); err != nil {
					return err
				}
				currentContext.Messages = append(currentContext.Messages, message)
				*newMessages = append(*newMessages, message)
			}
			pendingMessages = nil

			message, err := streamAssistantResponse(ctx, currentContext, config, emit, streamFn)
			if err != nil {
				return err
			}
			*newMessages = append(*newMessages, message)
			if message.StopReason == ai.StopReasonError || message.StopReason == ai.StopReasonAborted {
				_ = emit(ctx, AgentEvent{Type: "turn_end", Message: &message, ToolResults: []ai.Message{}})
				return emit(ctx, AgentEvent{Type: "agent_end", Messages: *newMessages})
			}

			toolResults, terminate, err := executeToolCalls(ctx, currentContext, message, config, emit)
			if err != nil {
				return err
			}
			hasMoreToolCalls = len(toolResults) > 0 && !terminate
			for _, result := range toolResults {
				currentContext.Messages = append(currentContext.Messages, result)
				*newMessages = append(*newMessages, result)
			}
			if err := emit(ctx, AgentEvent{Type: "turn_end", Message: &message, ToolResults: toolResults}); err != nil {
				return err
			}

			nextTurnContext := PrepareNextTurnContext{Message: message, ToolResults: toolResults, Context: *currentContext, NewMessages: *newMessages}
			if config.PrepareNextTurn != nil {
				next, err := config.PrepareNextTurn(ctx, nextTurnContext)
				if err != nil {
					return err
				}
				if next != nil {
					if next.Context != nil {
						*currentContext = *next.Context
					}
					if next.Model != nil {
						config.Model = *next.Model
					}
					if next.ThinkingLevel != nil {
						if *next.ThinkingLevel == ThinkingLevelOff {
							config.Reasoning = nil
						} else {
							config.Reasoning = next.ThinkingLevel
						}
					}
				}
			}
			if config.ShouldStopAfterTurn != nil {
				stopContext := ShouldStopAfterTurnContext{Message: message, ToolResults: toolResults, Context: *currentContext, NewMessages: *newMessages}
				stop, err := config.ShouldStopAfterTurn(ctx, stopContext)
				if err != nil {
					return err
				}
				if stop {
					return emit(ctx, AgentEvent{Type: "agent_end", Messages: *newMessages})
				}
			}
			pendingMessages, err = getMessages(ctx, config.GetSteeringMessages)
			if err != nil {
				return err
			}
		}
		followUps, err := getMessages(ctx, config.GetFollowUpMessages)
		if err != nil {
			return err
		}
		if len(followUps) == 0 {
			break
		}
		pendingMessages = followUps
	}
	return emit(ctx, AgentEvent{Type: "agent_end", Messages: *newMessages})
}

func streamAssistantResponse(ctx context.Context, agentContext *AgentContext, config AgentLoopConfig, emit AgentEventSink, streamFn StreamFn) (ai.Message, error) {
	messages := agentContext.Messages
	if config.TransformContext != nil {
		next, err := config.TransformContext(ctx, messages)
		if err != nil {
			return ai.Message{}, err
		}
		messages = next
	}
	llmMessages := []ai.Message(messages)
	if config.ConvertToLlm != nil {
		next, err := config.ConvertToLlm(messages)
		if err != nil {
			return ai.Message{}, err
		}
		llmMessages = next
	}
	options := ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{SessionID: config.SessionID, Transport: config.Transport, CacheRetention: config.CacheRetention, MaxRetryDelayMs: config.MaxRetryDelayMs, OnPayload: config.OnPayload, OnResponse: config.OnResponse}, ThinkingBudgets: config.ThinkingBudgets}
	if config.Reasoning != nil {
		level := ai.ThinkingLevel(*config.Reasoning)
		options.Reasoning = &level
	}
	if config.GetAPIKey != nil {
		key, err := config.GetAPIKey(ctx, config.Model.Provider)
		if err != nil {
			return ai.Message{}, err
		}
		options.APIKey = key
	}
	stream := streamFn(ctx, config.Model, ai.Context{SystemPrompt: agentContext.SystemPrompt, Messages: llmMessages, Tools: toAITools(agentContext.Tools)}, options)
	var final ai.Message
	addedPartial := false
	for event := range stream.Events() {
		if event.Partial != nil {
			partial := *event.Partial
			final = partial
			if event.Type == "start" {
				agentContext.Messages = append(agentContext.Messages, partial)
				addedPartial = true
				if err := emit(ctx, AgentEvent{Type: "message_start", Message: &partial}); err != nil {
					return ai.Message{}, err
				}
			} else if addedPartial {
				agentContext.Messages[len(agentContext.Messages)-1] = partial
				if err := emit(ctx, AgentEvent{Type: "message_update", Message: &partial, AssistantMessageEvent: &event}); err != nil {
					return ai.Message{}, err
				}
			}
		}
		if event.Message != nil {
			final = *event.Message
		}
		if event.Error != nil {
			final = *event.Error
		}
	}
	if final.Role == "" {
		final = stream.Result()
	}
	if !addedPartial {
		agentContext.Messages = append(agentContext.Messages, final)
		if err := emit(ctx, AgentEvent{Type: "message_start", Message: &final}); err != nil {
			if isAbortedContextEmission(ctx, final, err) {
				return final, nil
			}
			return ai.Message{}, err
		}
	} else {
		agentContext.Messages[len(agentContext.Messages)-1] = final
	}
	if err := emit(ctx, AgentEvent{Type: "message_end", Message: &final}); err != nil {
		if isAbortedContextEmission(ctx, final, err) {
			return final, nil
		}
		return ai.Message{}, err
	}
	return final, nil
}

func isAbortedContextEmission(ctx context.Context, message ai.Message, err error) bool {
	return message.StopReason == ai.StopReasonAborted && ctx.Err() != nil && errors.Is(err, context.Canceled)
}

func toAITools(tools []AgentTool[any]) []ai.Tool {
	out := make([]ai.Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Tool)
	}
	return out
}

func errorAssistant(model ai.Model, message string) ai.Message {
	return ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: ""}}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), StopReason: ai.StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
}

func getMessages(ctx context.Context, fn func(context.Context) ([]AgentMessage, error)) ([]AgentMessage, error) {
	if fn == nil {
		return nil, nil
	}
	messages, err := fn(ctx)
	if messages == nil {
		messages = []AgentMessage{}
	}
	return messages, err
}

func executeToolCalls(ctx context.Context, currentContext *AgentContext, assistantMessage ai.Message, config AgentLoopConfig, emit AgentEventSink) ([]ai.Message, bool, error) {
	toolCalls := messageToolCalls(assistantMessage)
	if len(toolCalls) == 0 {
		return nil, false, nil
	}
	if config.ToolExecution == ToolExecutionSequential || hasSequentialToolCall(currentContext, toolCalls) {
		return executeToolCallsSequential(ctx, currentContext, assistantMessage, toolCalls, config, emit)
	}
	return executeToolCallsParallel(ctx, currentContext, assistantMessage, toolCalls, config, emit)
}

func executeToolCallsSequential(ctx context.Context, currentContext *AgentContext, assistantMessage ai.Message, toolCalls []AgentToolCall, config AgentLoopConfig, emit AgentEventSink) ([]ai.Message, bool, error) {
	results := make([]ai.Message, 0, len(toolCalls))
	finalized := make([]AgentToolResult[any], 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		if err := emit(ctx, AgentEvent{Type: "tool_execution_start", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Args: toolCall.Arguments}); err != nil {
			return nil, false, err
		}
		result, isError := runToolCall(ctx, currentContext, assistantMessage, toolCall, config, emit)
		if err := emit(ctx, AgentEvent{Type: "tool_execution_end", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Result: result, IsError: isError}); err != nil {
			return nil, false, err
		}
		message := ai.Message{Role: "toolResult", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Content: result.Content, Details: result.Details, IsError: isError, Timestamp: time.Now().UnixMilli()}
		if err := emit(ctx, AgentEvent{Type: "message_start", Message: &message}); err != nil {
			return nil, false, err
		}
		if err := emit(ctx, AgentEvent{Type: "message_end", Message: &message}); err != nil {
			return nil, false, err
		}
		results = append(results, message)
		finalized = append(finalized, result)
	}
	return results, shouldTerminateToolBatch(finalized), nil
}

func executeToolCallsParallel(ctx context.Context, currentContext *AgentContext, assistantMessage ai.Message, toolCalls []AgentToolCall, config AgentLoopConfig, emit AgentEventSink) ([]ai.Message, bool, error) {
	finalized := make([]toolCallOutcome, len(toolCalls))
	var executable []int
	for index, toolCall := range toolCalls {
		if err := emit(ctx, AgentEvent{Type: "tool_execution_start", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Args: toolCall.Arguments}); err != nil {
			return nil, false, err
		}
		prepared, immediate, ok := prepareToolCall(ctx, currentContext, assistantMessage, toolCall, config)
		if ok {
			finalized[index] = toolCallOutcome{ToolCall: toolCall, Result: immediate.Result, IsError: immediate.IsError}
			if err := emit(ctx, AgentEvent{Type: "tool_execution_end", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Result: immediate.Result, IsError: immediate.IsError}); err != nil {
				return nil, false, err
			}
			continue
		}
		executable = append(executable, index)
		finalized[index] = toolCallOutcome{ToolCall: prepared.ToolCall, Prepared: prepared}
	}

	var wg sync.WaitGroup
	var emitMu sync.Mutex
	var firstErr error
	var firstErrMu sync.Mutex
	for _, index := range executable {
		index := index
		toolCall := toolCalls[index]
		wg.Add(1)
		go func() {
			defer wg.Done()
			prepared := finalized[index].Prepared
			result, isError := runPreparedToolCall(ctx, currentContext, assistantMessage, prepared, config, func(ctx context.Context, event AgentEvent) error {
				emitMu.Lock()
				defer emitMu.Unlock()
				return emit(ctx, event)
			})
			finalized[index] = toolCallOutcome{ToolCall: toolCall, Result: result, IsError: isError}
			emitMu.Lock()
			err := emit(ctx, AgentEvent{Type: "tool_execution_end", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Result: result, IsError: isError})
			emitMu.Unlock()
			if err != nil {
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstErrMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, false, firstErr
	}

	results := make([]ai.Message, 0, len(finalized))
	toolResults := make([]AgentToolResult[any], 0, len(finalized))
	for _, outcome := range finalized {
		message := ai.Message{Role: "toolResult", ToolCallID: outcome.ToolCall.ID, ToolName: outcome.ToolCall.Name, Content: outcome.Result.Content, Details: outcome.Result.Details, IsError: outcome.IsError, Timestamp: time.Now().UnixMilli()}
		if err := emit(ctx, AgentEvent{Type: "message_start", Message: &message}); err != nil {
			return nil, false, err
		}
		if err := emit(ctx, AgentEvent{Type: "message_end", Message: &message}); err != nil {
			return nil, false, err
		}
		results = append(results, message)
		toolResults = append(toolResults, outcome.Result)
	}
	return results, shouldTerminateToolBatch(toolResults), nil
}

type immediateToolCall struct {
	Result  AgentToolResult[any]
	IsError bool
}

type preparedToolCall struct {
	ToolCall AgentToolCall
	Tool     *AgentTool[any]
	Args     any
}

type toolCallOutcome struct {
	ToolCall AgentToolCall
	Prepared preparedToolCall
	Result   AgentToolResult[any]
	IsError  bool
}

func prepareToolCall(ctx context.Context, currentContext *AgentContext, assistantMessage ai.Message, toolCall AgentToolCall, config AgentLoopConfig) (preparedToolCall, immediateToolCall, bool) {
	var tool *AgentTool[any]
	for i := range currentContext.Tools {
		if currentContext.Tools[i].Name == toolCall.Name {
			tool = &currentContext.Tools[i]
			break
		}
	}
	if tool == nil {
		return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult("Tool " + toolCall.Name + " not found"), IsError: true}, true
	}
	args := any(toolCall.Arguments)
	if tool.PrepareArguments != nil {
		prepared, err := tool.PrepareArguments(args)
		if err != nil {
			return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult(err.Error()), IsError: true}, true
		}
		args = prepared
	}
	preparedArguments, ok := args.(map[string]any)
	if !ok {
		return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult("Tool arguments must be an object"), IsError: true}, true
	}
	validationToolCall := toolCall
	validationToolCall.Arguments = preparedArguments
	validatedArgs, err := aiutils.ValidateToolArguments(tool.Tool, validationToolCall)
	if err != nil {
		return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult(err.Error()), IsError: true}, true
	}
	args = validatedArgs
	if config.BeforeToolCall != nil {
		before, err := config.BeforeToolCall(ctx, BeforeToolCallContext{AssistantMessage: assistantMessage, ToolCall: toolCall, Args: args, Context: *currentContext})
		if err != nil {
			return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult(err.Error()), IsError: true}, true
		}
		if ctx.Err() != nil {
			return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult("Operation aborted"), IsError: true}, true
		}
		if before != nil && before.Block {
			reason := before.Reason
			if reason == "" {
				reason = "Tool execution was blocked"
			}
			return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult(reason), IsError: true}, true
		}
	}
	if ctx.Err() != nil {
		return preparedToolCall{}, immediateToolCall{Result: createErrorToolResult("Operation aborted"), IsError: true}, true
	}
	return preparedToolCall{ToolCall: toolCall, Tool: tool, Args: args}, immediateToolCall{}, false
}

func runToolCall(ctx context.Context, currentContext *AgentContext, assistantMessage ai.Message, toolCall AgentToolCall, config AgentLoopConfig, emit AgentEventSink) (AgentToolResult[any], bool) {
	prepared, immediate, ok := prepareToolCall(ctx, currentContext, assistantMessage, toolCall, config)
	if ok {
		return immediate.Result, immediate.IsError
	}
	return runPreparedToolCall(ctx, currentContext, assistantMessage, prepared, config, emit)
}

func runPreparedToolCall(ctx context.Context, currentContext *AgentContext, assistantMessage ai.Message, prepared preparedToolCall, config AgentLoopConfig, emit AgentEventSink) (AgentToolResult[any], bool) {
	toolCall := prepared.ToolCall
	var updateErr error
	var updateErrMu sync.Mutex
	result, err := prepared.Tool.Execute(ctx, toolCall.ID, prepared.Args, func(partialResult AgentToolResult[any]) {
		if err := emit(ctx, AgentEvent{Type: "tool_execution_update", ToolCallID: toolCall.ID, ToolName: toolCall.Name, Args: toolCall.Arguments, PartialResult: partialResult}); err != nil {
			updateErrMu.Lock()
			if updateErr == nil {
				updateErr = err
			}
			updateErrMu.Unlock()
		}
	})
	isError := false
	if err != nil {
		result = createErrorToolResult(err.Error())
		isError = true
	} else {
		updateErrMu.Lock()
		err = updateErr
		updateErrMu.Unlock()
		if err != nil {
			result = createErrorToolResult(err.Error())
			isError = true
		}
	}
	if config.AfterToolCall != nil {
		after, err := config.AfterToolCall(ctx, AfterToolCallContext{AssistantMessage: assistantMessage, ToolCall: toolCall, Args: prepared.Args, Result: result, IsError: isError, Context: *currentContext})
		if err != nil {
			return createErrorToolResult(err.Error()), true
		}
		if after != nil {
			if after.Content != nil {
				result.Content = after.Content
			}
			if after.Details != nil {
				result.Details = after.Details
			}
			if after.Terminate != nil {
				result.Terminate = *after.Terminate
			}
			if after.IsError != nil {
				isError = *after.IsError
			}
		}
	}
	return result, isError
}

func hasSequentialToolCall(currentContext *AgentContext, toolCalls []AgentToolCall) bool {
	for _, toolCall := range toolCalls {
		for _, tool := range currentContext.Tools {
			if tool.Name == toolCall.Name && tool.ExecutionMode == ToolExecutionSequential {
				return true
			}
		}
	}
	return false
}

func createErrorToolResult(message string) AgentToolResult[any] {
	return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: message}}, Details: map[string]any{}}
}

func shouldTerminateToolBatch(results []AgentToolResult[any]) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if !result.Terminate {
			return false
		}
	}
	return true
}

func messageToolCalls(message ai.Message) []AgentToolCall {
	blocks := contentBlocks(message.Content)
	toolCalls := make([]AgentToolCall, 0)
	for _, block := range blocks {
		if block.Type == "toolCall" {
			toolCalls = append(toolCalls, AgentToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: block.Arguments, ThoughtSignature: block.ThoughtSignature})
		}
	}
	return toolCalls
}

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
