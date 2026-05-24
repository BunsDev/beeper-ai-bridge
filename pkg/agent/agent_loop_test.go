package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestRunAgentLoopExecutesToolCallsAndContinues(t *testing.T) {
	ctx := context.Background()
	calls := 0
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "tool output"}}, Details: map[string]any{"ok": true}}, nil
		},
	}
	streamFn := func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), Timestamp: time.Now().UnixMilli()}
			if calls == 1 {
				message.StopReason = ai.StopReasonToolUse
				message.Content = []ai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "x"}}}
			} else {
				message.StopReason = ai.StopReasonStop
				message.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
			}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
		}()
		return stream
	}
	var events []string
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: []AgentTool[any]{tool}}, AgentLoopConfig{Model: testModel(), ToolExecution: ToolExecutionSequential}, func(ctx context.Context, event AgentEvent) error {
		events = append(events, event.Type)
		return nil
	}, streamFn)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 provider calls, got %d", calls)
	}
	if len(messages) != 4 {
		t.Fatalf("expected prompt, first assistant, tool result, final assistant; got %d messages", len(messages))
	}
	if messages[2].Role != "toolResult" || messages[2].ToolCallID != "call_1" {
		t.Fatalf("expected tool result as third message, got %#v", messages[2])
	}
	if !containsEvent(events, "tool_execution_start") || !containsEvent(events, "tool_execution_end") {
		t.Fatalf("expected tool execution events, got %#v", events)
	}
}

func TestAgentLoopReturnsEventStream(t *testing.T) {
	ctx := context.Background()
	stream := AgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{Model: testModel()}, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		out := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "done"}}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), StopReason: ai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
			out.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			out.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return out
	})
	var events []string
	for event := range stream.Events() {
		events = append(events, event.Type)
	}
	if err := stream.Error(); err != nil {
		t.Fatal(err)
	}
	messages := stream.Result()
	if len(messages) != 2 || messages[1].Role != "assistant" {
		t.Fatalf("expected prompt and assistant result, got %#v", messages)
	}
	if !containsEvent(events, "agent_start") || !containsEvent(events, "agent_end") {
		t.Fatalf("expected agent stream events, got %#v", events)
	}
}

func TestRunAgentLoopDefaultsToStreamSimple(t *testing.T) {
	ctx := context.Background()
	ai.ClearAPIProviders()
	defer ai.ClearAPIProviders()
	ai.RegisterAPIProvider(ai.Api("test-api"), func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		out := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "done"}}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), StopReason: ai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
			out.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			out.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return out
	})

	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: "hi", Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{Model: ai.Model{ID: "m", API: "test-api", Provider: "p"}}, func(ctx context.Context, event AgentEvent) error {
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].Role != "assistant" {
		t.Fatalf("expected default streamSimple assistant result, got %#v", messages)
	}
}

func TestRunAgentLoopDrainsFollowUpAfterNaturalStop(t *testing.T) {
	ctx := context.Background()
	calls := 0
	followUps := []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "follow up"}}, Timestamp: time.Now().UnixMilli()}}
	streamFn := func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), StopReason: ai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return stream
	}
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{
		Model: testModel(),
		GetFollowUpMessages: func(context.Context) ([]AgentMessage, error) {
			next := followUps
			followUps = nil
			return next, nil
		},
	}, func(context.Context, AgentEvent) error { return nil }, streamFn)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected follow-up to trigger second provider call, got %d", calls)
	}
	if len(messages) != 4 {
		t.Fatalf("expected prompt, assistant, follow-up, assistant; got %d", len(messages))
	}
	if messages[2].Role != "user" {
		t.Fatalf("expected follow-up user message at index 2, got %#v", messages[2])
	}
}

func TestRunAgentLoopExecutesParallelToolCallsConcurrently(t *testing.T) {
	ctx := context.Background()
	tools := []AgentTool[any]{
		sleepTool("slow_a", 120*time.Millisecond),
		sleepTool("slow_b", 120*time.Millisecond),
	}
	start := time.Now()
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: tools}, AgentLoopConfig{Model: testModel(), ToolExecution: ToolExecutionParallel, ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
		return len(turn.ToolResults) > 0, nil
	}}, func(ctx context.Context, event AgentEvent) error { return nil }, twoToolCallStreamFn())
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed >= 220*time.Millisecond {
		t.Fatalf("expected parallel tool calls to finish concurrently, elapsed %s", elapsed)
	}
	if len(messages) != 4 || messages[2].ToolName != "slow_a" || messages[3].ToolName != "slow_b" {
		t.Fatalf("expected ordered tool result messages, got %#v", messages)
	}
}

func TestRunAgentLoopUsesSequentialModeWhenToolRequiresIt(t *testing.T) {
	ctx := context.Background()
	sequential := sleepTool("slow_a", 80*time.Millisecond)
	sequential.ExecutionMode = ToolExecutionSequential
	tools := []AgentTool[any]{
		sequential,
		sleepTool("slow_b", 80*time.Millisecond),
	}
	start := time.Now()
	if _, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: tools}, AgentLoopConfig{Model: testModel(), ToolExecution: ToolExecutionParallel, ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
		return len(turn.ToolResults) > 0, nil
	}}, func(ctx context.Context, event AgentEvent) error { return nil }, twoToolCallStreamFn()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 140*time.Millisecond {
		t.Fatalf("expected sequential override to run tools serially, elapsed %s", elapsed)
	}
}

func TestRunAgentLoopEmitsToolExecutionUpdates(t *testing.T) {
	ctx := context.Background()
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "slow_a", Description: "slow_a"},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			onUpdate(AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "partial"}}})
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "done"}}}, nil
		},
	}
	updates := 0
	if _, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: []AgentTool[any]{tool, sleepTool("slow_b", 0)}}, AgentLoopConfig{Model: testModel(), ToolExecution: ToolExecutionSequential, ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
		return len(turn.ToolResults) > 0, nil
	}}, func(ctx context.Context, event AgentEvent) error {
		if event.Type == "tool_execution_update" {
			updates++
		}
		return nil
	}, twoToolCallStreamFn()); err != nil {
		t.Fatal(err)
	}
	if updates != 1 {
		t.Fatalf("expected one tool execution update, got %d", updates)
	}
}

func TestExecuteToolCallsSequentialStopsSchedulingAfterAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	secondExecuted := false
	currentContext := AgentContext{Tools: []AgentTool[any]{
		{
			Tool: ai.Tool{Name: "first", Description: "first", Parameters: map[string]any{"type": "object"}},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
				cancel()
				return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "first"}}}, nil
			},
		},
		{
			Tool: ai.Tool{Name: "second", Description: "second", Parameters: map[string]any{"type": "object"}},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
				secondExecuted = true
				return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "second"}}}, nil
			},
		},
	}}
	assistantMessage := ai.Message{Role: "assistant", Content: []ai.ContentBlock{
		{Type: "toolCall", ID: "call_1", Name: "first", Arguments: map[string]any{}},
		{Type: "toolCall", ID: "call_2", Name: "second", Arguments: map[string]any{}},
	}}

	results, _, err := executeToolCalls(ctx, &currentContext, assistantMessage, AgentLoopConfig{ToolExecution: ToolExecutionSequential}, func(context.Context, AgentEvent) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondExecuted || len(results) != 1 || results[0].ToolName != "first" {
		t.Fatalf("expected abort to stop scheduling after first tool, secondExecuted=%v results=%#v", secondExecuted, results)
	}
}

func TestRunAgentLoopTurnsToolUpdateEmitErrorIntoToolResult(t *testing.T) {
	ctx := context.Background()
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "slow_a", Description: "slow_a"},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			onUpdate(AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "partial"}}})
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "done"}}}, nil
		},
	}
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: []AgentTool[any]{tool}}, AgentLoopConfig{Model: testModel(), ToolExecution: ToolExecutionSequential, ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
		return len(turn.ToolResults) > 0, nil
	}}, func(ctx context.Context, event AgentEvent) error {
		if event.Type == "tool_execution_update" {
			return errors.New("update emit failed")
		}
		return nil
	}, singleToolCallStreamFn("slow_a", map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || !messages[2].IsError {
		t.Fatalf("expected errored tool result, got %#v", messages)
	}
	content := messages[2].Content.([]ai.ContentBlock)
	if content[0].Text != "update emit failed" {
		t.Fatalf("expected update emit error content, got %#v", content)
	}
}

func TestRunAgentLoopValidatesPreparedToolArguments(t *testing.T) {
	ctx := context.Background()
	executed := false
	sawBefore := false
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "count", Description: "count", Parameters: map[string]any{
			"type":     "object",
			"required": []any{"count"},
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
			},
		}},
		PrepareArguments: func(args any) (any, error) {
			return map[string]any{"count": "4"}, nil
		},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			executed = true
			values, ok := params.(map[string]any)
			if !ok || values["count"] != float64(4) {
				t.Fatalf("expected coerced integer args, got %#v", params)
			}
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}}, nil
		},
	}
	_, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: []AgentTool[any]{tool}}, AgentLoopConfig{
		Model:         testModel(),
		ToolExecution: ToolExecutionSequential,
		BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (*BeforeToolCallResult, error) {
			sawBefore = true
			values, ok := before.Args.(map[string]any)
			if !ok || values["count"] != float64(4) {
				t.Fatalf("expected beforeToolCall to receive validated args, got %#v", before.Args)
			}
			return nil, nil
		},
		ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return len(turn.ToolResults) > 0, nil
		},
	}, func(context.Context, AgentEvent) error { return nil }, singleToolCallStreamFn("count", map[string]any{"count": "bad"}))
	if err != nil {
		t.Fatal(err)
	}
	if !executed || !sawBefore {
		t.Fatalf("expected validated tool call to execute and beforeToolCall to run")
	}
}

func TestRunAgentLoopReturnsValidationErrorWithoutExecutingTool(t *testing.T) {
	ctx := context.Background()
	executed := false
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "count", Description: "count", Parameters: map[string]any{
			"type":     "object",
			"required": []any{"count"},
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
			},
		}},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			executed = true
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "unexpected"}}}, nil
		},
	}
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: []AgentTool[any]{tool}}, AgentLoopConfig{
		Model:         testModel(),
		ToolExecution: ToolExecutionSequential,
		ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return len(turn.ToolResults) > 0, nil
		},
	}, func(context.Context, AgentEvent) error { return nil }, singleToolCallStreamFn("count", map[string]any{"count": "bad"}))
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatalf("expected invalid tool call not to execute")
	}
	if len(messages) < 3 || !messages[2].IsError {
		t.Fatalf("expected error tool result, got %#v", messages)
	}
	content, ok := messages[2].Content.([]ai.ContentBlock)
	if !ok || len(content) == 0 || !strings.Contains(content[0].Text, "Validation failed for tool") {
		t.Fatalf("expected validation error content, got %#v", messages[2].Content)
	}
}

func TestRunAgentLoopAbortedBeforeToolExecutionReturnsToolError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	executed := false
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			executed = true
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "unexpected"}}}, nil
		},
	}
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{Tools: []AgentTool[any]{tool}}, AgentLoopConfig{
		Model:         testModel(),
		ToolExecution: ToolExecutionSequential,
		BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (*BeforeToolCallResult, error) {
			cancel()
			return nil, nil
		},
		ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return len(turn.ToolResults) > 0, nil
		},
	}, func(context.Context, AgentEvent) error { return nil }, singleToolCallStreamFn("read", map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatalf("tool executed after context cancellation")
	}
	if len(messages) != 3 || !messages[2].IsError {
		t.Fatalf("expected aborted tool result, got %#v", messages)
	}
	content := messages[2].Content.([]ai.ContentBlock)
	if content[0].Text != "Operation aborted" {
		t.Fatalf("expected Operation aborted, got %#v", content)
	}
}

func TestAgentSubscribeUnsubscribeRemovesOnlyTargetListener(t *testing.T) {
	ctx := context.Background()
	agent := NewAgent(AgentOptions{})
	firstCalls := 0
	secondCalls := 0
	unsubscribeFirst := agent.Subscribe(func(context.Context, AgentEvent) error {
		firstCalls++
		return nil
	})
	agent.Subscribe(func(context.Context, AgentEvent) error {
		secondCalls++
		return nil
	})
	unsubscribeFirst()
	if err := agent.processEvent(ctx, AgentEvent{Type: "turn_end"}); err != nil {
		t.Fatal(err)
	}
	if firstCalls != 0 {
		t.Fatalf("expected first listener to be removed, got %d calls", firstCalls)
	}
	if secondCalls != 1 {
		t.Fatalf("expected second listener to remain, got %d calls", secondCalls)
	}
}

func TestAgentContinueDrainsQueuedMessagesAfterAssistant(t *testing.T) {
	ctx := context.Background()
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{
			Model: testModel(),
			Messages: []AgentMessage{{
				Role:       "assistant",
				Content:    []ai.ContentBlock{{Type: "text", Text: "done"}},
				StopReason: ai.StopReasonStop,
			}},
			PendingToolCalls: map[string]bool{},
		},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			if len(llmContext.Messages) != 2 || llmContext.Messages[1].Role != "user" {
				t.Fatalf("expected queued user message after assistant, got %#v", llmContext.Messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &output})
			}()
			return stream
		},
	})
	agent.Steer(AgentMessage{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "queued"}}, Timestamp: time.Now().UnixMilli()})
	if err := agent.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if agent.HasQueuedMessages() {
		t.Fatalf("expected queued steering message to be drained")
	}
	if len(agent.State.Messages) != 3 {
		t.Fatalf("expected existing assistant, queued user, assistant response, got %#v", agent.State.Messages)
	}
}

func TestAgentContinueFromAssistantSkipsInitialSteeringPoll(t *testing.T) {
	ctx := context.Background()
	calls := 0
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{
			Model: testModel(),
			Messages: []AgentMessage{{
				Role:       "assistant",
				Content:    []ai.ContentBlock{{Type: "text", Text: "done"}},
				StopReason: ai.StopReasonStop,
			}},
			PendingToolCalls: map[string]bool{},
		},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			calls++
			if calls == 1 && len(llmContext.Messages) != 2 {
				t.Fatalf("expected only existing assistant plus first queued user, got %#v", llmContext.Messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &output})
			}()
			return stream
		},
	})
	agent.Steer(AgentMessage{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "first"}}, Timestamp: time.Now().UnixMilli()})
	agent.Steer(AgentMessage{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "second"}}, Timestamp: time.Now().UnixMilli()})
	if err := agent.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected second queued steering message to run after first continuation turn, got %d calls", calls)
	}
}

func TestAgentQueueModeAccessors(t *testing.T) {
	agent := NewAgent(AgentOptions{})
	agent.SetSteeringMode(QueueModeAll)
	if agent.GetSteeringMode() != QueueModeAll {
		t.Fatalf("expected steering mode all")
	}
	agent.SetFollowUpMode(QueueModeAll)
	if agent.GetFollowUpMode() != QueueModeAll {
		t.Fatalf("expected follow-up mode all")
	}
	agent.SetSteeringMode("")
	if agent.GetSteeringMode() != QueueModeOneAtATime {
		t.Fatalf("expected empty steering mode to reset to default")
	}
	agent.SetFollowUpMode("")
	if agent.GetFollowUpMode() != QueueModeOneAtATime {
		t.Fatalf("expected empty follow-up mode to reset to default")
	}
}

func TestAgentAbortCancelsActiveRunAndWaitForIdleSettles(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{Model: testModel(), PendingToolCalls: map[string]bool{}},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			close(started)
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				<-ctx.Done()
				output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: ""}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonAborted}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonAborted, Message: &output})
			}()
			return stream
		},
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- agent.Prompt(ctx, "hello")
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream to start")
	}
	agent.Abort()
	waitCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := agent.WaitForIdle(waitCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if agent.State.IsStreaming {
		t.Fatalf("expected agent to be idle")
	}
	if len(agent.State.Messages) != 2 || agent.State.Messages[1].StopReason != ai.StopReasonAborted {
		t.Fatalf("expected aborted assistant message, got %#v", agent.State.Messages)
	}
}

func TestAgentPromptConvertsRunFailureToAssistantMessage(t *testing.T) {
	ctx := context.Background()
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{Model: testModel(), PendingToolCalls: map[string]bool{}},
	})
	var events []string
	failOnce := true
	agent.Subscribe(func(ctx context.Context, event AgentEvent) error {
		events = append(events, event.Type)
		if event.Type == "turn_start" && failOnce {
			failOnce = false
			return errors.New("listener exploded")
		}
		return nil
	})
	if err := agent.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if agent.State.IsStreaming {
		t.Fatalf("expected agent to be idle")
	}
	if len(agent.State.Messages) != 1 {
		t.Fatalf("expected failure assistant message, got %#v", agent.State.Messages)
	}
	last := agent.State.Messages[0]
	if last.StopReason != ai.StopReasonError || last.ErrorMessage != "listener exploded" {
		t.Fatalf("expected failure assistant message, got %#v", last)
	}
	if agent.State.ErrorMessage != "listener exploded" {
		t.Fatalf("expected state error message, got %q", agent.State.ErrorMessage)
	}
	if !containsEvent(events, "agent_end") {
		t.Fatalf("expected agent_end event, got %#v", events)
	}
}

func TestAgentRunFailureClearsRuntimeState(t *testing.T) {
	ctx := context.Background()
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{Model: testModel(), PendingToolCalls: map[string]bool{}},
	})
	agent.State.StreamingMessage = &AgentMessage{Role: "assistant"}
	agent.State.PendingToolCalls["call_1"] = true
	agent.Subscribe(func(ctx context.Context, event AgentEvent) error {
		if event.Type == "turn_start" {
			return errors.New("listener exploded")
		}
		return nil
	})
	if err := agent.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if agent.State.IsStreaming || agent.State.StreamingMessage != nil || len(agent.State.PendingToolCalls) != 0 {
		t.Fatalf("expected runtime state cleared, got streaming=%v message=%#v pending=%#v", agent.State.IsStreaming, agent.State.StreamingMessage, agent.State.PendingToolCalls)
	}
}

func TestRunAgentLoopReturnsConvertToLlmError(t *testing.T) {
	ctx := context.Background()
	_, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: "hi", Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{
		Model: testModel(),
		ConvertToLlm: func(messages []AgentMessage) ([]ai.Message, error) {
			return nil, errors.New("convert exploded")
		},
	}, func(ctx context.Context, event AgentEvent) error {
		return nil
	}, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		t.Fatal("stream function should not be called")
		return nil
	})
	if err == nil || err.Error() != "convert exploded" {
		t.Fatalf("expected convert error, got %v", err)
	}
}

func TestAgentForwardsProviderPayloadAndResponseHooks(t *testing.T) {
	ctx := context.Background()
	payloadSeen := false
	responseSeen := false
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{Model: testModel(), PendingToolCalls: map[string]bool{}},
		OnPayload: func(payload any, model ai.Model) (any, bool, error) {
			payloadSeen = true
			if payload.(map[string]any)["original"] != true {
				t.Fatalf("unexpected payload %#v", payload)
			}
			return map[string]any{"patched": true}, true, nil
		},
		OnResponse: func(response ai.ProviderResponse, model ai.Model) error {
			responseSeen = true
			if response.Status != 201 || response.Headers["x-test"] != "ok" {
				t.Fatalf("unexpected response %#v", response)
			}
			return nil
		},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			if options.OnPayload == nil || options.OnResponse == nil {
				t.Fatal("expected provider hooks")
			}
			next, ok, err := options.OnPayload(map[string]any{"original": true}, model)
			if err != nil || !ok || next.(map[string]any)["patched"] != true {
				t.Fatalf("unexpected payload hook result %#v ok=%v err=%v", next, ok, err)
			}
			if err := options.OnResponse(ai.ProviderResponse{Status: 201, Headers: map[string]string{"x-test": "ok"}}, model); err != nil {
				t.Fatal(err)
			}
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &output})
			}()
			return stream
		},
	})
	if err := agent.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if !payloadSeen || !responseSeen {
		t.Fatalf("expected payload and response hooks, got payload=%v response=%v", payloadSeen, responseSeen)
	}
}

func TestAgentForwardsToolHooksFromOptions(t *testing.T) {
	ctx := context.Background()
	beforeSeen := false
	afterSeen := false
	tool := AgentTool[any]{
		Tool: ai.Tool{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "raw"}}}, nil
		},
	}
	agent := NewAgent(AgentOptions{
		InitialState: &AgentState{Model: testModel(), Tools: []AgentTool[any]{tool}, PendingToolCalls: map[string]bool{}},
		StreamFn:     singleToolCallStreamFn("read", map[string]any{}),
		BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (*BeforeToolCallResult, error) {
			beforeSeen = true
			return nil, nil
		},
		AfterToolCall: func(ctx context.Context, after AfterToolCallContext) (*AfterToolCallResult, error) {
			afterSeen = true
			return &AfterToolCallResult{Content: []ai.ContentBlock{{Type: "text", Text: "patched"}}}, nil
		},
	})
	if err := agent.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if !beforeSeen || !afterSeen {
		t.Fatalf("expected before/after tool hooks to be called")
	}
	if len(agent.State.Messages) < 3 {
		t.Fatalf("expected tool result message, got %#v", agent.State.Messages)
	}
	content := agent.State.Messages[2].Content.([]ai.ContentBlock)
	if content[0].Text != "patched" {
		t.Fatalf("expected after hook content patch, got %#v", content)
	}
}

func TestPrepareNextTurnReceivesTurnContext(t *testing.T) {
	ctx := context.Background()
	var seen *PrepareNextTurnContext
	streamFn := func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "done"}}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), StopReason: ai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return stream
	}
	_, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{
		Model: testModel(),
		PrepareNextTurn: func(ctx context.Context, next PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
			seen = &next
			return nil, nil
		},
		ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		},
	}, func(context.Context, AgentEvent) error { return nil }, streamFn)
	if err != nil {
		t.Fatal(err)
	}
	if seen == nil {
		t.Fatal("expected prepareNextTurn to receive turn context")
	}
	if seen.Message.Role != "assistant" || seen.Message.Content.([]ai.ContentBlock)[0].Text != "done" {
		t.Fatalf("unexpected prepareNextTurn message: %#v", seen.Message)
	}
	if len(seen.Context.Messages) != 2 || len(seen.NewMessages) != 2 {
		t.Fatalf("expected prompt and assistant in prepareNextTurn context, got context=%d new=%d", len(seen.Context.Messages), len(seen.NewMessages))
	}
}

func TestRunAgentLoopPropagatesAssistantStreamEmitErrors(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("emit failed")
	_, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{Model: testModel()}, func(ctx context.Context, event AgentEvent) error {
		if event.Type == "message_start" && event.Message != nil && event.Message.Role == "assistant" {
			return wantErr
		}
		return nil
	}, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "done"}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return stream
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected emit error to propagate, got %v", err)
	}
}

func TestRunAgentLoopEmitsMessageStartForTerminalOnlyProviderEvent(t *testing.T) {
	ctx := context.Background()
	var assistantEvents []string
	messages, err := RunAgentLoop(ctx, []AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}, Timestamp: time.Now().UnixMilli()}}, AgentContext{}, AgentLoopConfig{Model: testModel()}, func(ctx context.Context, event AgentEvent) error {
		if event.Message != nil && event.Message.Role == "assistant" && (event.Type == "message_start" || event.Type == "message_end") {
			assistantEvents = append(assistantEvents, event.Type)
		}
		return nil
	}, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonError, ErrorMessage: "boom", Timestamp: time.Now().UnixMilli()}
			stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonError, Error: &message})
		}()
		return stream
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].StopReason != ai.StopReasonError {
		t.Fatalf("expected terminal-only assistant error message, got %#v", messages)
	}
	if len(assistantEvents) != 2 || assistantEvents[0] != "message_start" || assistantEvents[1] != "message_end" {
		t.Fatalf("expected assistant start/end events, got %#v", assistantEvents)
	}
}

func testModel() ai.Model {
	return ai.Model{ID: "test", Name: "test", API: ai.ApiOpenAICompletions, Provider: "test", Input: []string{"text"}}
}

func sleepTool(name string, delay time.Duration) AgentTool[any] {
	return AgentTool[any]{
		Tool: ai.Tool{Name: name, Description: name},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[any]) (AgentToolResult[any], error) {
			if delay > 0 {
				time.Sleep(delay)
			}
			return AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: name + " output"}}}, nil
		},
	}
}

func twoToolCallStreamFn() StreamFn {
	calls := 0
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), Timestamp: time.Now().UnixMilli()}
			if calls == 1 {
				message.StopReason = ai.StopReasonToolUse
				message.Content = []ai.ContentBlock{
					{Type: "toolCall", ID: "call_a", Name: "slow_a", Arguments: map[string]any{}},
					{Type: "toolCall", ID: "call_b", Name: "slow_b", Arguments: map[string]any{}},
				}
			} else {
				message.StopReason = ai.StopReasonStop
				message.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
			}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
		}()
		return stream
	}
}

func singleToolCallStreamFn(name string, args map[string]any) StreamFn {
	calls := 0
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID, Usage: ai.EmptyUsage(), Timestamp: time.Now().UnixMilli()}
			if calls == 1 {
				message.StopReason = ai.StopReasonToolUse
				message.Content = []ai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: name, Arguments: args}}
			} else {
				message.StopReason = ai.StopReasonStop
				message.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
			}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &message})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
		}()
		return stream
	}
}

func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
