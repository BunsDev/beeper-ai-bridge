package matrix

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	agui "github.com/beeper/ai-bridge/pkg/ag-ui"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
)

func TestAnchorContentUsesVisibleTextAndAIProfile(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.Preview = aistream.Preview{Text: "visible preview"}

	content, extra := AnchorContent(*run)
	if content.MsgType != event.MsgText || content.Body != "visible preview" {
		t.Fatalf("bad anchor content: %#v", content)
	}
	if content.Format != event.FormatHTML || content.FormattedBody == "" {
		t.Fatalf("anchor preview should include Matrix HTML: %#v", content)
	}
	if content.BeeperPerMessageProfile == nil || content.BeeperPerMessageProfile.ID != "ai" || content.BeeperPerMessageProfile.Displayname != "AI" {
		t.Fatalf("missing AI per-message profile: %#v", content.BeeperPerMessageProfile)
	}
	ai, ok := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || ai.Message == nil || ai.Message.ID == "" || len(ai.Message.Parts) != 1 {
		t.Fatalf("bad compact AI message: %#v", extra[aistream.BeeperAIKey])
	}
	if ai.Kind != aistream.AIKindAnchor || ai.Protocol != "ag-ui" || ai.RunID != run.RunID {
		t.Fatalf("bad AI metadata: %#v", ai)
	}
}

func TestStreamingAnchorDoesNotIncludePreviewPart(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.Preview = aistream.Preview{}

	content, extra := AnchorContent(*run)
	if content.Body != "..." {
		t.Fatalf("empty streaming anchor should use placeholder body, got %q", content.Body)
	}
	ai, ok := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || ai.Message == nil || len(ai.Message.Parts) != 0 {
		t.Fatalf("streaming anchor should not include an initial text snapshot: %#v", extra[aistream.BeeperAIKey])
	}
}

func TestFinalContentIncludesAllPartsInlineWhenTheyFit(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Thinking("hidden reasoning")
	writer.Text("final **preview**")
	writer.Finish(agui.FinishReasonStop)

	content, extra := FinalContent(*run)
	if content.Format != event.FormatHTML || !strings.Contains(content.FormattedBody, "<strong>preview</strong>") {
		t.Fatalf("bad final Matrix HTML: %#v", content)
	}
	ai := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if ai.Message == nil || len(ai.Message.Parts) != 2 || ai.Message.Parts[0]["type"] != "thinking" || ai.Message.Parts[1]["type"] != "text" {
		t.Fatalf("final edit should include all fitting UI parts: %#v", ai.Message)
	}
	if ai.Final == nil || ai.Final.Delivery != "inline" || !ai.Final.PartsComplete || ai.Final.PartsRef != nil {
		t.Fatalf("small final payload should stay inline: %#v", ai.Final)
	}
}

func TestErrorFinalContentUsesGenericFallbackAndTerminalError(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Error("OpenAI API error (403): This model is not available")

	content, extra := FinalContent(*run)
	wantVisible := "OpenAI API error (403): This model is not available"
	if content.Body != wantVisible {
		t.Fatalf("error final body = %q, want %q", content.Body, wantVisible)
	}
	if strings.Contains(content.Body, aistream.ErrorFallbackText) {
		t.Fatalf("plain Matrix fallback should not duplicate generic error text: %#v", content.Body)
	}
	ai := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if len(ai.Events) != 1 || ai.Events[0].Event.Type() != agui.EventRunError {
		t.Fatalf("missing final RUN_ERROR event: %#v", ai.Events)
	}
	if ai.Events[0].Event.Get("message") != "OpenAI API error (403): This model is not available" {
		t.Fatalf("missing RUN_ERROR message: %#v", ai.Events[0].Event)
	}
	if ai.Message == nil || len(ai.Message.Parts) != 0 {
		t.Fatalf("terminal-only error final should have empty parts: %#v", ai.Message)
	}
	raw, err := json.Marshal(ai.Message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"parts":null`) || !strings.Contains(string(raw), `"parts":[]`) {
		t.Fatalf("empty UI parts should encode as an array: %s", raw)
	}
}

func TestFinalProjectionUsesAttachmentWhenFullPartsDoNotFit(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.MessageID = "msg-run-1"
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.ToolStart("tool-1", "search", 0, nil)
	writer.ToolEnd("tool-1", "search", map[string]any{"query": "beeper"}, map[string]any{
		"value": strings.Repeat("x", aistream.FinalMessageBudgetBytes*2),
	})
	writer.Text("done")
	writer.Finish(agui.FinishReasonStop)

	projection := ProjectFinal(*run, nil)
	ai := projection.Extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if !projection.NeedsAttachment {
		t.Fatal("oversized parts should require an attachment")
	}
	if ai.Message == nil || len(ai.Message.Parts) != 0 {
		t.Fatalf("attachment mode must not include partial inline parts: %#v", ai.Message)
	}
	if ai.Final == nil || ai.Final.Delivery != "attachment" || ai.Final.PartsComplete || ai.Final.PartsRef != nil {
		t.Fatalf("bad pending attachment metadata: %#v", ai.Final)
	}
	if size := finalPayloadSize(projection.Content, projection.Extra); size > aistream.FinalMessageBudgetBytes {
		t.Fatalf("final projection exceeded budget: size=%d budget=%d", size, aistream.FinalMessageBudgetBytes)
	}
}

func TestFinalProjectionIncludesPartsRefAfterUpload(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.MessageID = "msg-run-1"
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.ToolStart("tool-1", "search", 0, nil)
	writer.ToolEnd("tool-1", "search", nil, map[string]any{"value": strings.Repeat("x", aistream.FinalMessageBudgetBytes)})
	writer.Text("done")
	writer.Finish(agui.FinishReasonStop)
	ref := &aistream.FinalPartsRef{
		Schema:     aistream.FinalPartsRefSchema,
		MediaType:  aistream.FinalPartsMediaType,
		URL:        "mxc://example/final-parts",
		ByteSize:   123,
		SHA256:     "hash",
		PartsCount: len(run.FinalBeeperAIMessage(0, true).Parts),
	}

	projection := ProjectFinal(*run, ref)
	ai := projection.Extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if projection.NeedsAttachment {
		t.Fatal("provided partsRef should complete attachment projection")
	}
	if ai.Message == nil || len(ai.Message.Parts) != 0 {
		t.Fatalf("attachment projection should keep inline parts empty: %#v", ai.Message)
	}
	if ai.Final == nil || ai.Final.PartsRef == nil || ai.Final.PartsRef.URL != ref.URL {
		t.Fatalf("missing partsRef: %#v", ai.Final)
	}
}

func TestFinalProjectionSacrificesHTMLBeforeMetadata(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text(strings.Repeat("Use **bold** text in a long answer.\n\n", 500))
	writer.Finish(agui.FinishReasonStop)

	projection := ProjectFinal(*run, nil)
	ai := projection.Extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if ai.Final == nil || ai.Final.TextComplete == nil || *ai.Final.TextComplete {
		t.Fatalf("long HTML should report incomplete text: %#v", ai.Final)
	}
	if !strings.Contains(projection.Content.FormattedBody, "See more on supported clients") {
		t.Fatalf("incomplete Matrix HTML should include supported-client hint: %#v", projection.Content)
	}
	if !strings.Contains(projection.Content.FormattedBody, `data-beeper-ai-fallback="final-parts"`) {
		t.Fatalf("incomplete Matrix HTML should tag supported-client hint: %#v", projection.Content)
	}
	if size := finalPayloadSize(projection.Content, projection.Extra); size > aistream.FinalMessageBudgetBytes {
		t.Fatalf("final projection exceeded budget: size=%d budget=%d", size, aistream.FinalMessageBudgetBytes)
	}
}

func TestFinalProjectionUsesLeftoverBudgetForPlaintext(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text("Use **bold** markdown")
	writer.Finish(agui.FinishReasonStop)

	projection := ProjectFinal(*run, nil)
	if projection.Content.Body != run.Text() {
		t.Fatalf("small final payload should include plaintext fallback: %#v", projection.Content)
	}
}

func TestCarrierContentIsHiddenTextCarrierWithEvents(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	carrier := aistream.Carrier{Envelopes: []aistream.Envelope{{
		Seq: 1,
		Event: agui.NewEvent(map[string]any{
			"type":      agui.EventTextMessageContent,
			"messageId": run.MessageID,
			"delta":     "hello",
		}),
	}}}

	content, extra := CarrierContent(*run, carrier, id.EventID("$anchor"))
	if content.MsgType != event.MsgText || content.Body != "" {
		t.Fatalf("carrier should be empty m.text, got %#v", content)
	}
	if content.RelatesTo == nil || content.RelatesTo.EventID != "$anchor" {
		t.Fatalf("carrier should reference anchor, got %#v", content.RelatesTo)
	}
	ai, ok := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || ai.RunID != run.RunID || ai.Kind != aistream.AIKindStream {
		t.Fatalf("missing stream AI payload: %#v", extra)
	}
	if len(ai.Events) != 1 || ai.Events[0].Seq != 1 {
		t.Fatalf("missing events: %#v", extra)
	}
}

func TestApprovalContentIncludesContextAndChoices(t *testing.T) {
	ctx := aistream.ApprovalContext{
		ID:          "approval-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-run-1",
		ToolCallID:  "tool-1",
		ToolName:    "fetch",
		Title:       "Can I fetch this?",
		Description: "This will use the network.",
		ExpiresAt:   "2026-05-31T12:00:00Z",
		TargetEvent: "$anchor",
	}
	choices := aistream.DefaultApprovalChoices()

	content, extra := ApprovalContent(ctx, choices)
	if content.MsgType != event.MsgText || content.RelatesTo == nil || content.RelatesTo.EventID != "$anchor" || content.RelatesTo.Type != ApprovalRelationType {
		t.Fatalf("bad approval content: %#v", content)
	}
	meta, ok := extra[aistream.BeeperAIApprovalKey].(map[string]any)
	if !ok {
		t.Fatalf("missing approval metadata: %#v", extra)
	}
	if meta["schema"] != "com.beeper.ai.approval.v1" || meta["id"] != ctx.ID || meta["messageId"] != ctx.MessageID || meta["toolCallId"] != ctx.ToolCallID || meta["state"] != "requested" {
		t.Fatalf("bad approval metadata: %#v", meta)
	}
	if meta["title"] != ctx.Title || meta["description"] != ctx.Description || meta["expiresAt"] != ctx.ExpiresAt {
		t.Fatalf("approval metadata missing title/description/expiry: %#v", meta)
	}
	if _, ok := meta["runId"]; ok {
		t.Fatalf("approval event should not duplicate run metadata: %#v", meta)
	}
	if strings.Contains(strings.ToLower(content.Body), "react") || !strings.Contains(content.Body, "/approve approval-1 approve") {
		t.Fatalf("approval fallback should use approve commands, got %q", content.Body)
	}
	approvalChoices, ok := meta["choices"].([]any)
	if !ok || len(approvalChoices) != len(choices) {
		t.Fatalf("bad approval choices: %#v", meta["choices"])
	}
}
