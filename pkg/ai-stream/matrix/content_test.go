package matrix

import (
	"strings"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
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
	if ai.Message.Parts[0]["type"] != "text" || ai.Message.Parts[0]["content"] != "visible preview" {
		t.Fatalf("anchor AI message should include preview text part: %#v", ai.Message.Parts)
	}
	if ai.Kind != aistream.AIKindAnchor || ai.Protocol != "ag-ui" || ai.RunID != run.RunID {
		t.Fatalf("bad AI metadata: %#v", ai)
	}
}

func TestAnchorContentKeepsLongRunsCompact(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text(strings.Repeat("a", 70*1024))
	writer.Finish(agui.FinishReasonStop)

	content, extra := AnchorContent(*run)
	if len(content.Body) > aistream.PreviewBudgetBytes {
		t.Fatalf("anchor body length = %d, want <= %d", len(content.Body), aistream.PreviewBudgetBytes)
	}
	ai := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if ai.Message == nil {
		t.Fatalf("missing anchor AI message: %#v", ai)
	}
	if !ai.Preview.Truncated || len(ai.Preview.Text) > aistream.PreviewBudgetBytes {
		t.Fatalf("bad bounded preview: %#v", ai.Preview)
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

func TestAnchorContentRendersFinalPreviewAsMatrixHTML(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.Preview = aistream.Preview{Text: "Use **bold** and `code`"}

	content, _ := AnchorContent(*run)
	if content.Format != event.FormatHTML {
		t.Fatalf("format = %q, want Matrix HTML", content.Format)
	}
	if !strings.Contains(content.FormattedBody, "<strong>bold</strong>") || !strings.Contains(content.FormattedBody, "<code>code</code>") {
		t.Fatalf("formatted body did not render markdown: %q", content.FormattedBody)
	}
}

func TestFinalContentIncludesFinalUIParts(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Thinking("hidden reasoning")
	writer.Text("final **preview**")
	writer.Finish(agui.FinishReasonStop)

	content, extra := FinalContent(*run)
	if content.Body != "final **preview**" || content.Format != event.FormatHTML {
		t.Fatalf("bad final preview content: %#v", content)
	}
	ai, ok := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || ai.Message == nil || len(ai.Message.Parts) != 0 || ai.Final == nil || ai.Final.Delivery != "segmented" {
		t.Fatalf("final edit should keep parts out of the anchor payload: %#v", extra[aistream.BeeperAIKey])
	}
	projection := ProjectFinal(*run)
	parts := collectFinalSegmentParts(projection.Segments)
	if len(parts) != 2 || parts[0]["type"] != "thinking" || parts[1]["type"] != "text" {
		t.Fatalf("final segments must include concrete UI parts: %#v", parts)
	}
	if parts[0]["content"] != "hidden reasoning" || parts[1]["content"] == "" {
		t.Fatalf("final segments must preserve reasoning and text parts: %#v", parts)
	}
}

func TestFinalContentDoesNotTruncateUIParts(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	full := strings.Repeat("| Artifact | State | Latency |\n| --- | --- | --- |\n| renderer | active | accepts markdown |\n\n", 100)
	writer.Text(full)
	writer.Finish(agui.FinishReasonStop)
	expected := run.Text()

	projection := ProjectFinal(*run)
	extra := projection.Extra
	ai, ok := extra[aistream.BeeperAIKey].(aistream.BeeperAI)
	if !ok || ai.Message == nil || ai.Final == nil || ai.Final.Delivery != "segmented" {
		t.Fatalf("missing final UI message: %#v", extra[aistream.BeeperAIKey])
	}
	parts := collectFinalSegmentParts(projection.Segments)
	if len(parts) == 0 {
		t.Fatalf("missing final segment parts: %#v", projection.Segments)
	}
	textPart := parts[len(parts)-1]
	if textPart["content"] != expected {
		t.Fatalf("final UI text was truncated: got %d bytes want %d", len(textPart["content"].(string)), len(expected))
	}
	if metadata, ok := textPart["providerMetadata"]; ok {
		t.Fatalf("final UI text should not be marked truncated: %#v", metadata)
	}
}

func TestFinalProjectionPutsOverflowTextInHTMLOnlySegmentsBeforeParts(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text(strings.Repeat("Use **bold** text. ", 80))
	writer.Finish(agui.FinishReasonStop)

	_, segments, _ := projectFinal(*run, 4000)
	if len(segments) < 2 || segments[0].Text == "" {
		t.Fatalf("expected overflow text segment before final parts: %#v", segments)
	}
	content, _ := FinalSegmentContent(*run, segments[0], id.EventID("$anchor"))
	if content.Body != "" || content.FormattedBody == "" || !strings.Contains(content.FormattedBody, "<strong>bold</strong>") {
		t.Fatalf("overflow text segment should carry HTML without plain fallback: %#v", content)
	}
	for _, part := range segments[0].Message.Parts {
		if part["type"] == "text" && part["content"] == run.Text() {
			t.Fatalf("text segment should prefer Matrix HTML body before carrying final text parts: %#v", segments[0])
		}
	}
}

func TestFinalProjectionUsesEmptyBodyForPartOnlySegments(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Thinking("reasoning")
	writer.Text("short")
	writer.Finish(agui.FinishReasonStop)

	projection := ProjectFinal(*run)
	if len(projection.Segments) == 0 {
		t.Fatal("expected final parts to be delivered in segments")
	}
	for _, segment := range projection.Segments {
		if segment.Text != "" {
			t.Fatalf("short final text should fit the anchor, got text segment: %#v", segment)
		}
		content, _ := FinalSegmentContent(*run, segment, id.EventID("$anchor"))
		if content.Body != "" || content.FormattedBody != "" {
			t.Fatalf("part-only final segment should have empty Matrix body: %#v", content)
		}
	}
}

func collectFinalSegmentParts(segments []aistream.FinalSegment) []aistream.MessagePart {
	var parts []aistream.MessagePart
	for _, segment := range segments {
		parts = append(parts, segment.Message.Parts...)
	}
	return parts
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
	if _, ok := meta["runId"]; ok {
		t.Fatalf("approval event should not duplicate run metadata: %#v", meta)
	}
	approvalChoices, ok := meta["choices"].([]any)
	if !ok || len(approvalChoices) != len(choices) {
		t.Fatalf("bad approval choices: %#v", meta["choices"])
	}
	first := approvalChoices[0].(map[string]any)
	if first["key"] != aistream.ApprovalChoiceApprove || first["alias"] != "✅" {
		t.Fatalf("bad first approval choice: %#v", first)
	}
}
