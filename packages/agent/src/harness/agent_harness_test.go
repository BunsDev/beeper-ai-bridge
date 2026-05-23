package harness

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	agent "github.com/earendil-works/pi-mono/packages/agent/src"
	"github.com/earendil-works/pi-mono/packages/agent/src/harness/session"
	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestAgentHarnessPromptPersistsMessagesToSQLiteSession(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session:      session.NewSession(storage),
		Model:        harnessTestModel(),
		SystemPrompt: "system",
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			if options.SessionID != "session-1" {
				t.Fatalf("expected session id, got %q", options.SessionID)
			}
			if llmContext.SystemPrompt != "system" {
				t.Fatalf("expected system prompt, got %q", llmContext.SystemPrompt)
			}
			if len(llmContext.Messages) != 1 || llmContext.Messages[0].Role != "user" {
				t.Fatalf("unexpected llm context %#v", llmContext.Messages)
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
	if err != nil {
		t.Fatal(err)
	}

	var seen []string
	harness.Subscribe(func(ctx context.Context, event AgentHarnessEvent) error {
		seen = append(seen, event.Type)
		return nil
	})
	message, err := harness.Prompt(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if message.Role != "assistant" {
		t.Fatalf("expected assistant, got %#v", message)
	}
	entries, err := storage.GetEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected user and assistant persisted, got %d", len(entries))
	}
	if !containsHarnessEvent(seen, "save_point") || !containsHarnessEvent(seen, "settled") {
		t.Fatalf("expected save_point and settled events, got %#v", seen)
	}
}

func TestAgentHarnessNextTurnAndSetters(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	var captured [][]ai.Message
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session:       session.NewSession(storage),
		Model:         harnessTestModel(),
		ThinkingLevel: agent.ThinkingLevelHigh,
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			captured = append(captured, append([]ai.Message{}, llmContext.Messages...))
			if options.Reasoning == nil || *options.Reasoning != ai.ThinkingLevelHigh {
				t.Fatalf("expected high reasoning, got %#v", options.Reasoning)
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
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.NextTurn(ctx, "queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Prompt(ctx, "prompt"); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 || len(captured[0]) != 2 {
		t.Fatalf("expected queued and prompt messages, got %#v", captured)
	}
	if err := harness.SetModel(ctx, ai.Model{ID: "next", API: ai.ApiOpenAIResponses, Provider: "openai"}); err != nil {
		t.Fatal(err)
	}
	if harness.GetModel().ID != "next" {
		t.Fatalf("expected model setter to update model")
	}
	if err := harness.SetThinkingLevel(ctx, agent.ThinkingLevelOff); err != nil {
		t.Fatal(err)
	}
	if harness.GetThinkingLevel() != agent.ThinkingLevelOff {
		t.Fatalf("expected thinking setter to update level")
	}
	harness.SetSteeringMode(agent.QueueModeAll)
	if harness.GetSteeringMode() != agent.QueueModeAll {
		t.Fatalf("expected steering mode setter to update mode")
	}
	harness.SetFollowUpMode(agent.QueueModeAll)
	if harness.GetFollowUpMode() != agent.QueueModeAll {
		t.Fatalf("expected follow-up mode setter to update mode")
	}
}

func TestAgentHarnessSkillAndPromptFromTemplateExecuteTurns(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	var captured [][]ai.Message
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
		Resources: AgentHarnessResources{
			Skills: []Skill{{
				Name:        "reader",
				Description: "Read things",
				Content:     "Use rg first.",
				FilePath:    "/repo/skills/reader/SKILL.md",
			}},
			PromptTemplates: []PromptTemplate{{
				Name:    "fix",
				Content: "Fix $1 with $ARGUMENTS",
			}},
		},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			captured = append(captured, append([]ai.Message{}, llmContext.Messages...))
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &output})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Skill(ctx, "reader", "Only inspect files."); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.PromptFromTemplate(ctx, "fix", []string{"bug", "now"}); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 2 {
		t.Fatalf("expected skill and prompt template turns, got %#v", captured)
	}
	first := textFromContent(captured[0][0].Content)
	if first == "" || !containsText(first, "<skill name=\"reader\"") || !containsText(first, "Only inspect files.") {
		t.Fatalf("expected formatted skill invocation, got %q", first)
	}
	second := textFromContent(captured[1][2].Content)
	if second != "Fix bug with bug now" {
		t.Fatalf("expected prompt template substitution, got %q", second)
	}
}

func TestAgentHarnessUnsubscribeRemovesOnlyTargetHandlers(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	harness, err := NewAgentHarness(AgentHarnessOptions{Session: session.NewSession(storage), Model: harnessTestModel()})
	if err != nil {
		t.Fatal(err)
	}

	firstSubscriberCalls := 0
	secondSubscriberCalls := 0
	removeSubscriber := harness.Subscribe(func(context.Context, AgentHarnessEvent) error {
		firstSubscriberCalls++
		return nil
	})
	harness.Subscribe(func(context.Context, AgentHarnessEvent) error {
		secondSubscriberCalls++
		return nil
	})
	firstHandlerCalls := 0
	secondHandlerCalls := 0
	removeHandler := harness.On("resources_update", func(context.Context, AgentHarnessEvent) (any, error) {
		firstHandlerCalls++
		return nil, nil
	})
	harness.On("resources_update", func(context.Context, AgentHarnessEvent) (any, error) {
		secondHandlerCalls++
		return nil, nil
	})
	removeSubscriber()
	removeHandler()
	if err := harness.SetResources(ctx, AgentHarnessResources{}); err != nil {
		t.Fatal(err)
	}
	if firstSubscriberCalls != 0 || firstHandlerCalls != 0 {
		t.Fatalf("expected removed callbacks to stay idle, got subscribers=%d handlers=%d", firstSubscriberCalls, firstHandlerCalls)
	}
	if secondSubscriberCalls != 1 || secondHandlerCalls != 1 {
		t.Fatalf("expected retained callbacks once, got subscribers=%d handlers=%d", secondSubscriberCalls, secondHandlerCalls)
	}
}

func TestAgentHarnessSetActiveToolsControlsTurnContext(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	var capturedTools [][]ai.Tool
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
		Tools: []agent.AgentTool[any]{
			{Tool: ai.Tool{Name: "read", Description: "read"}},
			{Tool: ai.Tool{Name: "write", Description: "write"}},
		},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			capturedTools = append(capturedTools, append([]ai.Tool{}, llmContext.Tools...))
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &output})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetActiveTools([]string{"write"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if len(capturedTools) != 1 || len(capturedTools[0]) != 1 || capturedTools[0][0].Name != "write" {
		t.Fatalf("expected only active write tool, got %#v", capturedTools)
	}
	if err := harness.SetActiveTools([]string{"missing"}); err == nil {
		t.Fatalf("expected unknown active tool to fail")
	}
}

func TestAgentHarnessHookResultsPatchTurnContextAndTools(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	toolCalls := 0
	tool := agent.AgentTool[any]{
		Tool: ai.Tool{Name: "read", Description: "read"},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
			toolCalls++
			return agent.AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "raw"}}}, nil
		},
	}
	var capturedContexts []ai.Context
	streamCalls := 0
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
		Tools:   []agent.AgentTool[any]{tool},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			streamCalls++
			capturedContexts = append(capturedContexts, llmContext)
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID}
				if streamCalls == 1 {
					output.StopReason = ai.StopReasonToolUse
					output.Content = []ai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "x"}}}
				} else {
					output.StopReason = ai.StopReasonStop
					output.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
				}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.On("before_agent_start", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		return BeforeAgentStartResult{
			SystemPrompt: "hook system",
			Messages:     []agent.AgentMessage{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "extra"}}, Timestamp: 1}},
		}, nil
	})
	harness.On("context", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		messages := append([]agent.AgentMessage{}, event.Messages...)
		messages = append(messages, agent.AgentMessage{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "context hook"}}, Timestamp: 2})
		return ContextResult{Messages: messages}, nil
	})
	harness.On("tool_call", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		if event.ToolName != "read" {
			t.Fatalf("expected read tool event, got %#v", event)
		}
		return ToolCallResult{}, nil
	})
	terminate := true
	harness.On("tool_result", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		return ToolResultPatch{Content: []ai.ContentBlock{{Type: "text", Text: "patched"}}, Details: map[string]any{"patched": true}, Terminate: &terminate}, nil
	})

	if _, err := harness.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if toolCalls != 1 {
		t.Fatalf("expected tool to execute once, got %d", toolCalls)
	}
	if len(capturedContexts) != 1 {
		t.Fatalf("expected terminate patch to stop after first tool turn, got %d stream calls", len(capturedContexts))
	}
	if capturedContexts[0].SystemPrompt != "hook system" {
		t.Fatalf("expected hooked system prompt, got %q", capturedContexts[0].SystemPrompt)
	}
	if len(capturedContexts[0].Messages) != 3 {
		t.Fatalf("expected prompt, before-start extra, and context hook messages, got %#v", capturedContexts[0].Messages)
	}
	entries, err := storage.GetEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var patched bool
	for _, raw := range entries {
		var entry struct {
			Type    string          `json:"type"`
			Message ai.Message      `json:"message"`
			Raw     json.RawMessage `json:"-"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Type == "message" && entry.Message.Role == "toolResult" && textFromContent(entry.Message.Content) == "patched" {
			patched = true
		}
	}
	if !patched {
		t.Fatalf("expected patched tool result entry, got %#v", entries)
	}
}

func TestAgentHarnessPendingSessionWritesRefreshNextToolTurn(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	nextModel := ai.Model{ID: "next", API: ai.ApiOpenAIResponses, Provider: "openai"}
	var streamModels []string
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
		Tools: []agent.AgentTool[any]{{
			Tool: ai.Tool{Name: "read", Description: "read"},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
				return agent.AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "tool"}}}, nil
			},
		}},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			streamModels = append(streamModels, model.ID)
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID}
				if len(streamModels) == 1 {
					output.StopReason = ai.StopReasonToolUse
					output.Content = []ai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "x"}}}
				} else {
					output.StopReason = ai.StopReasonStop
					output.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
				}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	var savePointPending []bool
	harness.Subscribe(func(ctx context.Context, event AgentHarnessEvent) error {
		if event.Type == "message_end" && event.Message != nil && event.Message.Role == "assistant" && !changed {
			changed = true
			if err := harness.SetModel(ctx, nextModel); err != nil {
				return err
			}
		}
		if event.Type == "save_point" {
			savePointPending = append(savePointPending, event.HadPendingMutations)
		}
		return nil
	})
	if _, err := harness.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if len(streamModels) != 2 || streamModels[0] != "gpt-test" || streamModels[1] != "next" {
		t.Fatalf("expected next tool turn to use pending model, got %#v", streamModels)
	}
	if len(savePointPending) == 0 || !savePointPending[0] {
		t.Fatalf("expected save_point to report pending mutations, got %#v", savePointPending)
	}
	entries, err := storage.FindEntries(ctx, "model_change")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected queued model change to persist once, got %d", len(entries))
	}
}

func TestAgentHarnessSystemPromptCallbackRefreshesAtSavePoint(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	secondModel := ai.Model{ID: "second", API: ai.ApiOpenAIResponses, Provider: "openai", Reasoning: true}
	var captured []string
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session:       session.NewSession(storage),
		Model:         harnessTestModel(),
		ThinkingLevel: agent.ThinkingLevelOff,
		Resources: AgentHarnessResources{Skills: []Skill{{
			Name:        "prompt",
			Description: "prompt",
			Content:     "first prompt",
			FilePath:    "/skills/prompt",
		}}},
		SystemPromptFunc: func(ctx context.Context, promptContext AgentHarnessSystemPromptContext) (string, error) {
			if len(promptContext.Resources.Skills) == 0 {
				return "missing prompt", nil
			}
			return promptContext.Resources.Skills[0].Content, nil
		},
		Tools: []agent.AgentTool[any]{{
			Tool: ai.Tool{Name: "calculate", Description: "calculate"},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
				return agent.AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "2"}}}, nil
			},
		}},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			captured = append(captured, llmContext.SystemPrompt)
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				output := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID}
				if len(captured) == 1 {
					output.StopReason = ai.StopReasonToolUse
					output.Content = []ai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "calculate", Arguments: map[string]any{}}}
				} else {
					output.StopReason = ai.StopReasonStop
					output.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
				}
				stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated := false
	harness.Subscribe(func(ctx context.Context, event AgentHarnessEvent) error {
		if event.Type == "tool_execution_start" && !updated {
			updated = true
			if err := harness.SetModel(ctx, secondModel); err != nil {
				return err
			}
			return harness.SetResources(ctx, AgentHarnessResources{Skills: []Skill{{
				Name:        "prompt",
				Description: "prompt",
				Content:     "second prompt",
				FilePath:    "/skills/prompt",
			}}})
		}
		return nil
	})
	if _, err := harness.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 2 || captured[0] != "first prompt" || captured[1] != "second prompt" {
		t.Fatalf("expected refreshed system prompts, got %#v", captured)
	}
}

func TestAgentHarnessToolCallHookCanBlockTool(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	toolCalls := 0
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
		Tools: []agent.AgentTool[any]{{
			Tool: ai.Tool{Name: "read", Description: "read"},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
				toolCalls++
				return agent.AgentToolResult[any]{Content: []ai.ContentBlock{{Type: "text", Text: "raw"}}}, nil
			},
		}},
		StreamFn: singleToolCallStreamFn(),
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.On("tool_call", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		return ToolCallResult{Block: true, Reason: "blocked"}, nil
	})
	if _, err := harness.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if toolCalls != 0 {
		t.Fatalf("expected blocked tool not to execute")
	}
	entries, err := storage.GetEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sessionEntriesContainToolResult(entries, "blocked") {
		t.Fatalf("expected blocked tool result entry, got %#v", entries)
	}
}

func TestAgentHarnessProviderHooksPatchOptionsPayloadAndObserveResponse(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	timeout := 123
	maxRetries := 4
	var payloadSeen any
	var responseSeen ai.ProviderResponse
	var optionsSeen ai.SimpleStreamOptions
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			optionsSeen = options
			if options.OnPayload != nil {
				next, ok, err := options.OnPayload(map[string]any{"original": true}, model)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Fatal("expected payload hook to patch payload")
				}
				payloadSeen = next
			}
			if options.OnResponse != nil {
				if err := options.OnResponse(ai.ProviderResponse{Status: 201, Headers: map[string]string{"x-test": "ok"}}, model); err != nil {
					t.Fatal(err)
				}
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
	if err != nil {
		t.Fatal(err)
	}
	harness.On("before_provider_request", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		return BeforeProviderRequestResult{StreamOptions: AgentHarnessStreamOptions{TimeoutMs: &timeout, MaxRetries: &maxRetries, Headers: map[string]string{"x-hook": "yes"}, Metadata: map[string]any{"hook": true}}}, nil
	})
	harness.On("before_provider_payload", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		payload := event.Payload.(map[string]any)
		payload["patched"] = true
		return BeforeProviderPayloadResult{Payload: payload}, nil
	})
	harness.On("after_provider_response", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		responseSeen = event.ProviderResponse
		return nil, nil
	})

	if _, err := harness.Prompt(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if optionsSeen.TimeoutMs == nil || *optionsSeen.TimeoutMs != timeout || optionsSeen.MaxRetries == nil || *optionsSeen.MaxRetries != maxRetries {
		t.Fatalf("expected patched request options, got %#v", optionsSeen)
	}
	if optionsSeen.Headers["x-hook"] != "yes" || optionsSeen.Metadata["hook"] != true {
		t.Fatalf("expected patched headers/metadata, got headers=%#v metadata=%#v", optionsSeen.Headers, optionsSeen.Metadata)
	}
	if payloadSeen.(map[string]any)["patched"] != true {
		t.Fatalf("expected patched payload, got %#v", payloadSeen)
	}
	if responseSeen.Status != 201 || responseSeen.Headers["x-test"] != "ok" {
		t.Fatalf("expected observed response, got %#v", responseSeen)
	}
}

func TestAgentHarnessAbortCancelsActiveRunAndClearsQueues(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	started := make(chan struct{})
	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: session.NewSession(storage),
		Model:   harnessTestModel(),
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
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	queued := false
	harness.Subscribe(func(ctx context.Context, event AgentHarnessEvent) error {
		events = append(events, event.Type)
		if event.Type == "message_start" && !queued {
			queued = true
			if err := harness.Steer(ctx, "queued steer"); err != nil {
				return err
			}
			if err := harness.FollowUp(ctx, "queued follow-up"); err != nil {
				return err
			}
		}
		return nil
	})
	errCh := make(chan error, 1)
	go func() {
		_, err := harness.Prompt(ctx, "hello")
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream to start")
	}
	result, err := harness.Abort(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ClearedFollowUp) != 1 {
		t.Fatalf("expected abort to clear queued follow-up, got %#v", result)
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := harness.WaitForIdle(waitCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if !containsHarnessEvent(events, "abort") {
		t.Fatalf("expected abort event, got %#v", events)
	}
}

func TestAgentHarnessCompactPersistsCompaction(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	sess := session.NewSession(storage)
	if _, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "start", Timestamp: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "middle"}}, Usage: ai.Usage{Input: 10, Output: 5}, StopReason: ai.StopReasonStop, Timestamp: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "recent", Timestamp: 3}); err != nil {
		t.Fatal(err)
	}

	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session:  sess,
		Model:    harnessTestModel(),
		StreamFn: summaryTestStreamFn(t, "summary", &ai.Context{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	harness.Subscribe(func(ctx context.Context, event AgentHarnessEvent) error {
		events = append(events, event.Type)
		return nil
	})
	result, err := harness.Compact(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "summary" {
		t.Fatalf("unexpected compact result %#v", result)
	}
	if !containsHarnessEvent(events, "session_compact") {
		t.Fatalf("expected session_compact event, got %#v", events)
	}
	entries, err := storage.FindEntries(ctx, "compaction")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected compaction entry, got %d", len(entries))
	}
}

func TestAgentHarnessCompactHookCanCancelOrProvideCompaction(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	sess := session.NewSession(storage)
	if _, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "old", Timestamp: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "middle"}}, Usage: ai.Usage{Input: 10, Output: 5}, StopReason: ai.StopReasonStop, Timestamp: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "new", Timestamp: 3}); err != nil {
		t.Fatal(err)
	}
	harness, err := NewAgentHarness(AgentHarnessOptions{Session: sess, Model: harnessTestModel(), StreamFn: summaryTestStreamFn(t, "model summary", &ai.Context{})})
	if err != nil {
		t.Fatal(err)
	}
	remove := harness.On("session_before_compact", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		if event.CompactionPreparation == nil || len(event.BranchEntries) == 0 {
			t.Fatalf("expected compaction preparation and branch entries")
		}
		return SessionBeforeCompactResult{Cancel: true}, nil
	})
	if _, err := harness.Compact(ctx, ""); err == nil {
		t.Fatal("expected compaction cancellation error")
	}
	remove()
	harness.On("session_before_compact", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		return SessionBeforeCompactResult{Compaction: &CompactResult{Summary: "hook summary", FirstKeptEntryID: event.CompactionPreparation.FirstKeptEntryID, TokensBefore: event.CompactionPreparation.TokensBefore, Details: map[string]any{"hook": true}}}, nil
	})
	result, err := harness.Compact(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "hook summary" {
		t.Fatalf("expected hook summary, got %#v", result)
	}
	rawEntries, err := storage.FindEntries(ctx, "compaction")
	if err != nil {
		t.Fatal(err)
	}
	var compactEntry map[string]any
	if err := json.Unmarshal(rawEntries[len(rawEntries)-1], &compactEntry); err != nil {
		t.Fatal(err)
	}
	if compactEntry["fromHook"] != true {
		t.Fatalf("expected fromHook on compaction entry, got %#v", compactEntry)
	}
}

func TestAgentHarnessNavigateTreeMovesLeafAndCreatesSummary(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	sess := session.NewSession(storage)
	rootID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "root", Timestamp: 1})
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "toolCall", Name: "read", Arguments: map[string]any{"path": "/tmp/a"}}}, Timestamp: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.MoveTo(ctx, &rootID, nil); err != nil {
		t.Fatal(err)
	}
	targetID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "edit me"}}, Timestamp: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetLeafID(ctx, &oldID); err != nil {
		t.Fatal(err)
	}

	harness, err := NewAgentHarness(AgentHarnessOptions{
		Session: sess,
		Model:   harnessTestModel(),
		GenerateBranchSummary: func(ctx context.Context, prep BranchPreparation) (BranchSummaryResult, error) {
			if len(prep.Messages) != 1 || prep.Messages[0].Role != "assistant" {
				t.Fatalf("unexpected branch prep %#v", prep)
			}
			readFiles, modifiedFiles := ComputeFileLists(prep.FileOps)
			return BranchSummaryResult{Summary: "branch summary", ReadFiles: readFiles, ModifiedFiles: modifiedFiles}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := harness.NavigateTree(ctx, targetID, NavigateTreeOptions{Summarize: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.EditorText != "edit me" {
		t.Fatalf("expected editor text from target user message, got %q", result.EditorText)
	}
	if len(result.SummaryEntry) == 0 {
		t.Fatalf("expected summary entry")
	}
	leaf, err := sess.GetLeafID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if leaf == nil || *leaf != rootID {
		var summaryEntry SessionEntry
		if err := json.Unmarshal(result.SummaryEntry, &summaryEntry); err != nil {
			t.Fatal(err)
		}
		if *leaf != summaryEntry.ID {
			t.Fatalf("expected leaf to summary entry, got %#v", leaf)
		}
	}
	summaries, err := storage.FindEntries(ctx, "branch_summary")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected branch summary entry, got %d", len(summaries))
	}
}

func TestAgentHarnessNavigateTreeHookCanCancelOrProvideSummary(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	sess := session.NewSession(storage)
	rootID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "root", Timestamp: 1})
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "old branch"}}, Timestamp: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.MoveTo(ctx, &rootID, nil); err != nil {
		t.Fatal(err)
	}
	targetID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "target", Timestamp: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetLeafID(ctx, &oldID); err != nil {
		t.Fatal(err)
	}
	harness, err := NewAgentHarness(AgentHarnessOptions{Session: sess, Model: harnessTestModel()})
	if err != nil {
		t.Fatal(err)
	}
	remove := harness.On("session_before_tree", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		if event.BranchPreparation == nil || event.BranchPreparation.TargetID != targetID {
			t.Fatalf("expected tree preparation for target, got %#v", event.BranchPreparation)
		}
		return SessionBeforeTreeResult{Cancel: true}, nil
	})
	cancelled, err := harness.NavigateTree(ctx, targetID, NavigateTreeOptions{Summarize: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled.Cancelled {
		t.Fatalf("expected cancelled navigation")
	}
	remove()
	harness.On("session_before_tree", func(ctx context.Context, event AgentHarnessEvent) (any, error) {
		return SessionBeforeTreeResult{Summary: &BranchSummaryResult{Summary: "hook branch summary", ReadFiles: []string{"/tmp/a"}}}, nil
	})
	result, err := harness.NavigateTree(ctx, targetID, NavigateTreeOptions{Summarize: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SummaryEntry) == 0 {
		t.Fatalf("expected hook summary entry")
	}
	var entry map[string]any
	if err := json.Unmarshal(result.SummaryEntry, &entry); err != nil {
		t.Fatal(err)
	}
	if entry["summary"] != "hook branch summary" || entry["fromHook"] != true {
		t.Fatalf("expected hook branch summary entry, got %#v", entry)
	}
}

func harnessTestModel() ai.Model {
	return ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai"}
}

func containsHarnessEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}

func containsText(value string, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}

func singleToolCallStreamFn() agent.StreamFn {
	calls := 0
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			output := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID}
			if calls == 1 {
				output.StopReason = ai.StopReasonToolUse
				output.Content = []ai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "x"}}}
			} else {
				output.StopReason = ai.StopReasonStop
				output.Content = []ai.ContentBlock{{Type: "text", Text: "done"}}
			}
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
		}()
		return stream
	}
}

func sessionEntriesContainToolResult(entries []json.RawMessage, text string) bool {
	for _, raw := range entries {
		var entry struct {
			Type    string     `json:"type"`
			Message ai.Message `json:"message"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		if entry.Type == "message" && entry.Message.Role == "toolResult" && textFromContent(entry.Message.Content) == text {
			return true
		}
	}
	return false
}
