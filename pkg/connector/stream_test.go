package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestStreamPublisherUsesFakeProviderAndPublishesDeltas(t *testing.T) {
	ctx := context.Background()
	testAPI := ai.Api("test-stream")
	ai.RegisterAPIProvider(testAPI, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "hello"}}, StopReason: ai.StopReasonStop}
			stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "hel"})
			stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "lo"})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return stream
	})
	defer ai.UnregisterAPIProvider(testAPI)

	publisher := &recordingStreamPublisher{}
	client := &Client{}
	run := aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.MessageID = "assistant:run"
	secondVisibleChunks := 0
	streamFn := client.streamPublisher(publisher, "!room:example.com", "$event", run, func() {
		secondVisibleChunks++
	})
	result := streamFn(ctx, ai.Model{ID: "fake", API: testAPI}, ai.Context{}, ai.SimpleStreamOptions{}).Result()
	if result.StopReason != ai.StopReasonStop {
		t.Fatalf("unexpected stream result %#v", result)
	}
	if secondVisibleChunks != 1 {
		t.Fatalf("expected one second-visible-chunk callback, got %d", secondVisibleChunks)
	}
	if len(publisher.updates) != 4 {
		t.Fatalf("expected stream updates, got %#v", publisher.updates)
	}
	aiPayload, ok := publisher.updates[1][aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || len(aiPayload.Events) != 2 {
		t.Fatalf("unexpected first delta %#v", publisher.updates[1])
	}
	part := aiPayload.Events[1].Event
	if part.Type() != agui.EventTextMessageContent || part.Get("delta") != "hel" {
		t.Fatalf("unexpected first text part %#v", part)
	}
}

func TestStreamPublisherFailsAfterIdleTimeout(t *testing.T) {
	ctx := context.Background()
	oldTimeout := activeStreamIdleTimeout
	activeStreamIdleTimeout = 20 * time.Millisecond
	defer func() {
		activeStreamIdleTimeout = oldTimeout
	}()
	testAPI := ai.Api("test-stream-timeout")
	ai.RegisterAPIProvider(testAPI, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			<-ctx.Done()
			stream.End()
		}()
		return stream
	})
	defer ai.UnregisterAPIProvider(testAPI)

	publisher := &recordingStreamPublisher{}
	client := &Client{}
	run := aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.MessageID = "assistant:run"
	result := client.streamPublisher(publisher, "!room:example.com", "$event", run)(ctx, ai.Model{ID: "fake", API: testAPI}, ai.Context{}, ai.SimpleStreamOptions{}).Result()

	if result.StopReason != ai.StopReasonError || !strings.Contains(result.ErrorMessage, "timed out") {
		t.Fatalf("expected timeout error result, got %#v", result)
	}
	if run.Status.State != "error" {
		t.Fatalf("expected run to be marked error, got %#v", run.Status)
	}
	if len(publisher.updates) < 2 {
		t.Fatalf("expected start and timeout stream updates, got %#v", publisher.updates)
	}
}

func TestStreamPublisherReusesRunAcrossToolContinuation(t *testing.T) {
	ctx := context.Background()
	toolAPI := ai.Api("test-stream-tool")
	answerAPI := ai.Api("test-stream-answer")
	ai.RegisterAPIProvider(toolAPI, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			toolCall := &ai.ToolCall{ID: "call-session", Name: "get_session", Arguments: map[string]any{}}
			message := ai.Message{
				Role:       "assistant",
				Content:    []ai.ContentBlock{{Type: "toolCall", ID: toolCall.ID, Name: toolCall.Name, Arguments: toolCall.Arguments}},
				StopReason: ai.StopReasonToolUse,
				Usage:      ai.Usage{Input: 10, Output: 1, ReasoningTokens: 2, TotalTokens: 11},
			}
			stream.Push(ai.AssistantMessageEvent{Type: "toolcall_start", ToolCall: toolCall})
			stream.Push(ai.AssistantMessageEvent{Type: "toolcall_end", ToolCall: toolCall})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonToolUse, Message: &message})
		}()
		return stream
	})
	ai.RegisterAPIProvider(answerAPI, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{
				Role:       "assistant",
				Content:    []ai.ContentBlock{{Type: "text", Text: "hello"}},
				StopReason: ai.StopReasonStop,
				Usage:      ai.Usage{Input: 3, Output: 2, ReasoningTokens: 1, TotalTokens: 5},
			}
			stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "hello"})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return stream
	})
	defer ai.UnregisterAPIProvider(toolAPI)
	defer ai.UnregisterAPIProvider(answerAPI)

	publisher := &recordingStreamPublisher{}
	client := &Client{}
	run := aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.MessageID = "assistant:run"
	cursor := &streamPublishCursor{nextSeq: 1}

	toolResult := client.streamPublisherWithEndFrom(publisher, "!room:example.com", "$event", run, cursor, nil)(ctx, ai.Model{ID: "fake", API: toolAPI}, ai.Context{}, ai.SimpleStreamOptions{}).Result()
	if toolResult.StopReason != ai.StopReasonToolUse {
		t.Fatalf("unexpected tool stream result %#v", toolResult)
	}
	for _, evt := range run.Events {
		if evt.Type() == agui.EventRunFinished {
			t.Fatalf("tool-use provider stop must not finish a continued AG-UI run: %#v", run.Events)
		}
		if evt.Type() == agui.EventToolCallResult {
			t.Fatalf("provider toolcall_end must not emit a tool result before the tool runs: %#v", run.Events)
		}
	}
	answerResult := client.streamPublisherWithEndFrom(publisher, "!room:example.com", "$event", run, cursor, nil)(ctx, ai.Model{ID: "fake", API: answerAPI}, ai.Context{}, ai.SimpleStreamOptions{}).Result()
	if answerResult.StopReason != ai.StopReasonStop {
		t.Fatalf("unexpected answer stream result %#v", answerResult)
	}

	runStarted := 0
	for _, evt := range run.Events {
		if evt.Type() == agui.EventRunStarted {
			runStarted++
		}
	}
	if runStarted != 1 {
		t.Fatalf("expected one run start event, got %d in %#v", runStarted, run.Events)
	}
	runFinished := 0
	for _, evt := range run.Events {
		if evt.Type() == agui.EventRunFinished {
			runFinished++
		}
	}
	if runFinished != 1 {
		t.Fatalf("expected one terminal run finish after continuation, got %d in %#v", runFinished, run.Events)
	}
	if run.Usage != (agui.Usage{PromptTokens: 13, CompletionTokens: 3, ReasoningTokens: 3, TotalTokens: 16}) {
		t.Fatalf("continued run usage was not accumulated: %#v", run.Usage)
	}
	message := run.FinalBeeperAIMessage(0, true)
	if message.ID != "assistant:run" || len(message.Parts) != 2 {
		t.Fatalf("expected one assistant UI message with text and tool parts, got %#v", message)
	}
	if message.Parts[0]["type"] != "tool-call" || message.Parts[0]["toolCallId"] != "call-session" {
		t.Fatalf("expected folded tool-call part first, got %#v", message.Parts)
	}
	if message.Parts[1]["type"] != "text" || message.Parts[1]["content"] != "hello" {
		t.Fatalf("expected final answer text after tool call, got %#v", message.Parts)
	}
}

func TestFinalizedAssistantRunPreservesAccumulatedStreamUsage(t *testing.T) {
	run := *aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.Usage = agui.Usage{PromptTokens: 13, CompletionTokens: 3, ReasoningTokens: 3, TotalTokens: 16}
	message := ai.Message{
		Role:       "assistant",
		Content:    []ai.ContentBlock{{Type: "text", Text: "hello"}},
		StopReason: ai.StopReasonStop,
		Usage:      ai.Usage{Input: 3, Output: 2, ReasoningTokens: 1, TotalTokens: 5},
	}

	final := finalizedAssistantRun(run, message)
	if final.Usage != run.Usage {
		t.Fatalf("finalization overwrote accumulated usage: got %#v want %#v", final.Usage, run.Usage)
	}
}

func TestFinalizedAssistantRunErrorUsesGenericPreviewAndTerminalReason(t *testing.T) {
	run := *aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	message := ai.Message{
		Role:         "assistant",
		StopReason:   ai.StopReasonError,
		ErrorMessage: "OpenAI API error (403): This model is not available",
	}

	final := finalizedAssistantRun(run, message)
	wantVisible := message.ErrorMessage
	if final.Preview.Text != wantVisible {
		t.Fatalf("error preview = %q, want %q", final.Preview.Text, wantVisible)
	}
	payload := final.AI(aistream.AIKindFinal)
	if len(payload.Events) != 1 || payload.Events[0].Event.Type() != agui.EventRunError {
		t.Fatalf("missing final RUN_ERROR event: %#v", payload.Events)
	}
	if payload.Events[0].Event.Get("message") != message.ErrorMessage {
		t.Fatalf("missing RUN_ERROR message: %#v", payload.Events[0].Event)
	}
}

func TestDoneEventAddsFinalTextWhenProviderDidNotStreamDeltas(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	message := ai.Message{
		Role:       "assistant",
		Content:    []ai.ContentBlock{{Type: "text", Text: "final only"}},
		StopReason: ai.StopReasonStop,
	}

	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})

	uiMessage := run.FinalBeeperAIMessage(0, true)
	if got := len(uiMessage.Parts); got != 1 {
		t.Fatalf("expected final text part, got %d parts: %#v", got, uiMessage.Parts)
	}
	if uiMessage.Parts[0]["type"] != "text" || uiMessage.Parts[0]["content"] != "final only" {
		t.Fatalf("final-only provider text was not preserved as a UI part: %#v", uiMessage.Parts)
	}
}

func TestAppendToolOutputsPreservesStructuredResult(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/gpt-5", "assistant:run", "GPT-5", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	writer.ToolStart("call-session", "get_session", 0, nil)
	writer.ToolEnd("call-session", "get_session", map[string]any{}, nil)

	appendToolOutputs(run, []toolOutputEvent{{
		ID:    "call-session",
		Name:  "get_session",
		Input: map[string]any{},
		Result: agent.AgentToolResult[any]{
			Content: []ai.ContentBlock{{Type: "text", Text: `{"session_id":"session-1"}`}},
			Details: struct {
				SessionID string `json:"session_id"`
				ModelID   string `json:"model_id"`
			}{SessionID: "session-1", ModelID: "gpt-5"},
		},
	}})

	message := run.FinalBeeperAIMessage(0, true)
	if len(message.Parts) != 1 {
		t.Fatalf("expected one tool part, got %#v", message.Parts)
	}
	output, ok := message.Parts[0]["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected object output, got %#v", message.Parts[0]["output"])
	}
	if output["session_id"] != "session-1" || output["model_id"] != "gpt-5" || output["state"] != agui.ToolResultStateComplete || output["status"] != "success" {
		encoded, _ := json.Marshal(output)
		t.Fatalf("tool output lost structured result: %s", encoded)
	}
}

func TestAppendToolOutputsAddsWebSearchSources(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/gpt-5", "assistant:run", "GPT-5", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	writer.ToolStart("call-search", "web_search", 0, nil)
	writer.ToolEnd("call-search", "web_search", map[string]any{"query": "q"}, nil)

	appendToolOutputs(run, []toolOutputEvent{{
		ID:    "call-search",
		Name:  "web_search",
		Input: map[string]any{"query": "q"},
		Result: agent.AgentToolResult[any]{
			Details: map[string]any{
				"results": []any{
					map[string]any{
						"id":              "doc_1",
						"title":           "One",
						"url":             "https://example.com/one",
						"description":     "desc",
						"published":       "2026-01-01",
						"siteName":        "Example",
						"highlights":      []any{"hit"},
						"highlightScores": []any{0.5},
						"summary":         "sum",
						"subpages":        []any{map[string]any{"title": "Sub", "url": "https://example.com/sub"}},
						"extras":          map[string]any{"links": []any{"https://example.com/link"}},
					},
				},
			},
		},
	}})

	message := run.FinalBeeperAIMessage(0, true)
	var source map[string]any
	for _, part := range message.Parts {
		if part["type"] == "source-url" {
			source = part
			break
		}
	}
	if source == nil {
		t.Fatalf("expected source-url part, got %#v", message.Parts)
	}
	if source["url"] != "https://example.com/one" || source["title"] != "One" || source["sourceId"] != "doc_1" {
		t.Fatalf("unexpected source part %#v", source)
	}
	meta, ok := source["providerMetadata"].(map[string]any)
	if !ok || meta["description"] != "desc" || meta["published"] != "2026-01-01" || meta["siteName"] != "Example" {
		t.Fatalf("missing source metadata: %#v", source["providerMetadata"])
	}
	if meta["summary"] != "sum" || meta["highlights"] == nil || meta["highlightScores"] == nil || meta["subpages"] == nil || meta["extras"] == nil {
		t.Fatalf("missing rich source metadata: %#v", source["providerMetadata"])
	}
}

func TestAssistantEventMetadataCanBeFinalizedBeforeInsert(t *testing.T) {
	client := &Client{}
	run := aistream.NewRun("run", "thread", "beeper/gpt-5", "assistant:run", "GPT-5", timeNow())
	run.MessageID = "assistant:run"
	assistantEvent, metadata := client.assistantEvent(
		context.Background(),
		aiid.PortalKey(id.RoomID("!room:example.com"), "login"),
		"assistant:run",
		"beeper",
		"gpt-5",
		"run",
		&event.BeeperStreamInfo{Type: "com.beeper.ai.response"},
		*run,
	)
	if metadata.StreamStatus != "streaming" {
		t.Fatalf("expected streaming metadata, got %#v", metadata)
	}
	if assistantEvent.Sender.Sender != aiid.AssistantUserID() {
		t.Fatalf("assistant event used sender %q", assistantEvent.Sender.Sender)
	}
	profile := assistantEvent.Data.Parts[0].Content.BeeperPerMessageProfile
	if profile == nil || profile.ID != string(aiid.ModelContactID("beeper", "gpt-5")) || profile.Displayname != "gpt-5" || !profile.HasFallback {
		t.Fatalf("assistant event missing model profile: %#v", profile)
	}
	fillAssistantMetadata(metadata, "entry", "beeper", "gpt-5", "run", ai.Message{
		Role:       "assistant",
		ResponseID: "resp",
		Usage:      ai.Usage{Input: 1, Output: 2},
		StopReason: ai.StopReasonStop,
	})
	partMetadata, ok := assistantEvent.Data.Parts[0].DBMetadata.(*aiid.MessageMetadata)
	if !ok {
		t.Fatalf("unexpected metadata type %T", assistantEvent.Data.Parts[0].DBMetadata)
	}
	if partMetadata.SessionEntryID != "entry" || partMetadata.StreamStatus != "done" || partMetadata.ResponseID != "resp" {
		t.Fatalf("metadata was not finalized through shared pointer: %#v", partMetadata)
	}

	edit := client.assistantFinalEdit(aiid.PortalKey(id.RoomID("!room:example.com"), "login"), "assistant:run", "beeper", "gpt-5", *run, ai.Message{
		Role:       "assistant",
		StopReason: ai.StopReasonStop,
	}, metadata)
	if edit.Sender.Sender != aiid.AssistantUserID() {
		t.Fatalf("assistant final edit used sender %q", edit.Sender.Sender)
	}
	converted, err := edit.ConvertEditFunc(context.Background(), nil, nil, []*database.Message{{}}, edit.Data)
	if err != nil {
		t.Fatal(err)
	}
	profile = converted.ModifiedParts[0].Content.BeeperPerMessageProfile
	if profile == nil || profile.ID != string(aiid.ModelContactID("beeper", "gpt-5")) || profile.Displayname != "gpt-5" || !profile.HasFallback {
		t.Fatalf("assistant final edit missing model profile: %#v", profile)
	}
}

func TestAssistantModelProfileUsesConfiguredModelDisplayName(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:     "custom",
		Models: []ai.Model{{ID: "gpt-5.5", Name: "GPT 5.5"}},
	}
	client := &Client{Main: &Connector{}, UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}}
	content := &event.MessageEventContent{}
	client.applyModelProfile(context.Background(), content, "custom", "gpt-5.5")
	profile := content.BeeperPerMessageProfile
	if profile == nil || profile.ID != string(aiid.ModelContactID("custom", "gpt-5.5")) || profile.Displayname != "GPT 5.5" || !profile.HasFallback {
		t.Fatalf("assistant model profile lost configured display name: %#v", profile)
	}
}

func TestAssistantModelProfileUsesCatalogDisplayName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"type":"com.beeper.ai.model_list","data":[{"id":"openai/gpt-5.5","name":"GPT 5.5 Catalog","provider":{"id":"wpcom_openai","model_id":"gpt-5.5","api":"openai-responses"}}]}`))
	}))
	defer server.Close()

	client := &Client{
		Main: &Connector{AppServiceToken: "as-token"},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			UserMXID: "@test:beeper.com",
			Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{aiid.DefaultProvider: {
				ID:           aiid.DefaultProvider,
				DisplayName:  "Beeper AI",
				API:          ai.ApiOpenAIResponses,
				Provider:     ai.ProviderOpenAI,
				BaseURL:      server.URL + "/proxy/openai/v1",
				DefaultModel: "beeper/default",
			}}},
		}},
	}
	content := &event.MessageEventContent{}
	client.applyModelProfile(context.Background(), content, "beeper", "openai/gpt-5.5")
	profile := content.BeeperPerMessageProfile
	if profile == nil || profile.ID != string(aiid.ModelContactID("beeper", "openai/gpt-5.5")) || profile.Displayname != "GPT 5.5 Catalog" || !profile.HasFallback {
		t.Fatalf("assistant model profile ignored catalog display name: %#v", profile)
	}
}

func TestApplyAIStreamDonePublishesProviderUsageInFinalAGUIEvents(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/gpt-5.5", "assistant:run", "GPT-5.5", timeNow())
	writer := aistream.NewWriter(run, timeNow)

	writer.Start()
	writer.Text("hello")
	applyAIStreamEvent(writer, ai.AssistantMessageEvent{
		Type:   "done",
		Reason: ai.StopReasonStop,
		Message: &ai.Message{
			Usage: ai.Usage{Input: 10, Output: 5, ReasoningTokens: 4, TotalTokens: 15},
		},
	}, 400000)

	want := agui.Usage{PromptTokens: 10, CompletionTokens: 5, ReasoningTokens: 4, TotalTokens: 15, ContextLimit: 400000}
	if run.Usage != want {
		t.Fatalf("run usage = %#v, want %#v", run.Usage, want)
	}
	var finished agui.Usage
	for _, evt := range run.Events {
		if evt.Type() == agui.EventRunFinished {
			finished = evt.Get("usage").(agui.Usage)
		}
	}
	if finished != want {
		t.Fatalf("RUN_FINISHED usage = %#v, want %#v", finished, want)
	}
}

func TestAGUIFinishReasonFromAIMapsAbortedToCancelled(t *testing.T) {
	if got := aguiFinishReasonFromAI(ai.StopReasonAborted); got != agui.FinishReasonCancelled {
		t.Fatalf("aborted finish reason = %q, want %q", got, agui.FinishReasonCancelled)
	}
}

func TestApplyAIStreamEventStreamsToolCallsFromPartialContent(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/gpt-5.5", "assistant:run", "GPT-5.5", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	partial := &ai.Message{
		Role: "assistant",
		Content: []ai.ContentBlock{{
			Type:      "toolCall",
			ID:        "call-1",
			Name:      "read_file",
			Arguments: map[string]any{"path": "README.md"},
		}},
	}

	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: 0, Partial: partial})
	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: 0, Delta: `{"path":"README.md"}`, Partial: partial})

	var sawStart, sawArgs bool
	for _, evt := range run.Events {
		switch evt.Type() {
		case agui.EventToolCallStart:
			sawStart = evt.Get("toolCallId") == "call-1" && evt.Get("toolName") == "read_file"
		case agui.EventToolCallArgs:
			sawArgs = evt.Get("toolCallId") == "call-1" && evt.Get("delta") == `{"path":"README.md"}`
			if args, ok := evt.Get("args").(map[string]any); !ok || args["path"] != "README.md" {
				t.Fatalf("expected streamed tool args, got %#v", evt.Get("args"))
			}
		}
	}
	if !sawStart || !sawArgs {
		t.Fatalf("missing streamed tool lifecycle events: %#v", run.Events)
	}
}

func TestApplyAIStreamEventDoesNotPublishReasoningSignaturesToAGUI(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/gpt-5.5", "assistant:run", "GPT-5.5", timeNow())
	writer := aistream.NewWriter(run, timeNow)
	partial := &ai.Message{
		Role: "assistant",
		Content: []ai.ContentBlock{{
			Type:              "thinking",
			Thinking:          "hidden continuity",
			ThinkingSignature: "opaque-reasoning-state",
		}},
	}

	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "thinking_start", ContentIndex: 0, Partial: partial})
	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "thinking_delta", ContentIndex: 0, Delta: "hidden continuity", Partial: partial})
	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "thinking_end", ContentIndex: 0, Content: "hidden continuity", Partial: partial})

	for _, evt := range run.Events {
		if evt.Type() == agui.EventReasoningEncrypted {
			t.Fatalf("reasoning signatures must not be published as AG-UI events: %#v", run.Events)
		}
	}
}

func TestApplyAIStreamEventIgnoresRawProviderEvent(t *testing.T) {
	run := aistream.NewRun("run", "thread", "beeper/gpt-5.5", "assistant:run", "GPT-5.5", timeNow())
	writer := aistream.NewWriter(run, timeNow)
	raw := map[string]any{"type": "response.created", "response": map[string]any{"id": "resp-1"}}

	applyAIStreamEvent(writer, ai.AssistantMessageEvent{Type: "raw", RawEvent: raw, RawSource: "openai"})

	if len(run.Events) != 0 {
		t.Fatalf("raw provider events must not be published as AG-UI events: %#v", run.Events)
	}
}

func TestPublishToolOutputStreamsLiveResult(t *testing.T) {
	ctx := context.Background()
	publisher := &recordingStreamPublisher{}
	run := aistream.NewRun("run", "thread", "beeper/gpt-5.5", "assistant:run", "GPT-5.5", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	writer.ToolStart("call-1", "get_session", 0, nil)
	active := &activeAIRun{streams: []*assistantStreamState{{
		eventID: "$event",
		run:     run,
		publish: streamPublishCursor{nextSeq: 1, published: len(run.Events)},
	}}}

	err := active.publishToolOutput(ctx, &Client{}, publisher, "!room:example.com", toolOutputEvent{
		ID:    "call-1",
		Name:  "get_session",
		Input: map[string]any{},
		Result: agent.AgentToolResult[any]{
			Content: []ai.ContentBlock{{Type: "text", Text: `{"session_id":"session-1"}`}},
			Details: map[string]any{
				"session_id": "session-1",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(publisher.updates) != 1 {
		t.Fatalf("expected one live stream update, got %#v", publisher.updates)
	}
	aiPayload, ok := publisher.updates[0][aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || len(aiPayload.Events) != 2 {
		t.Fatalf("unexpected live stream carrier %#v", publisher.updates[0])
	}
	part := aiPayload.Events[1].Event
	if part.Type() != agui.EventToolCallResult || part.Get("toolCallId") != "call-1" {
		t.Fatalf("expected live tool result event, got %#v", part)
	}
	result, ok := part.Get("content").(string)
	if !ok || result == "" {
		t.Fatalf("expected encoded tool result, got %#v", part.Get("content"))
	}
}

func TestPublishNewStreamEventsSuppressesMautrixRequestBodyLogs(t *testing.T) {
	logger := zerolog.New(io.Discard).Level(zerolog.DebugLevel)
	ctx := logger.WithContext(context.Background())
	publisher := &recordingStreamPublisher{}
	run := aistream.NewRun("run", "thread", "beeper/gpt-5.5", "assistant:run", "GPT-5.5", timeNow())
	run.MessageID = "assistant:run"
	writer := aistream.NewWriter(run, timeNow)
	writer.Start()
	writer.Text("hello")

	err := (&Client{}).publishNewStreamEvents(ctx, publisher, "!room:example.com", "$event", run, &streamPublishCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(publisher.publishLogLevels) != 1 {
		t.Fatalf("expected one stream publish, got %d", len(publisher.publishLogLevels))
	}
	if publisher.publishLogLevels[0] < zerolog.FatalLevel {
		t.Fatalf("stream carrier publish should suppress mautrix request body logs, got level %s", publisher.publishLogLevels[0])
	}
}

type recordingStreamPublisher struct {
	updates          []map[string]any
	publishLogLevels []zerolog.Level
}

func (p *recordingStreamPublisher) NewDescriptor(ctx context.Context, roomID id.RoomID, streamType string) (*event.BeeperStreamInfo, error) {
	return &event.BeeperStreamInfo{UserID: "@bot:example.com", Type: streamType}, nil
}

func (p *recordingStreamPublisher) Register(ctx context.Context, roomID id.RoomID, eventID id.EventID, descriptor *event.BeeperStreamInfo) error {
	return nil
}

func (p *recordingStreamPublisher) Publish(ctx context.Context, roomID id.RoomID, eventID id.EventID, delta map[string]any) error {
	p.updates = append(p.updates, delta)
	p.publishLogLevels = append(p.publishLogLevels, zerolog.Ctx(ctx).GetLevel())
	return nil
}

func (p *recordingStreamPublisher) Unregister(roomID id.RoomID, eventID id.EventID) {
}

func timeNow() time.Time {
	return time.Unix(1, 0)
}
