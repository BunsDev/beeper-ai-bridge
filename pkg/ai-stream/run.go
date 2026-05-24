package aistream

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
)

const (
	BeeperAIKey          = "com.beeper.ai"
	BeeperAIMetadataKey  = "com.beeper.ai.metadata"
	BeeperAIStreamKey    = "com.beeper.llm"
	BeeperAIStreamDeltas = BeeperAIStreamKey + ".deltas"
	FinalPartsCustomName = "com.beeper.ai.final-parts"
	DefaultModel         = "dummybridge/ag-ui"
	CarrierBudgetBytes   = 40 * 1024
	PreviewBudgetBytes   = 4096
	SnapshotTextBytes    = 4096
)

type Run struct {
	ThreadID   string
	RunID      string
	MessageID  string
	Model      string
	AgentID    string
	AgentName  string
	Events     []agui.Event
	Approvals  []ApprovalSummary
	Artifacts  ArtifactSummary
	Data       map[string]any
	Status     Status
	Usage      agui.Usage
	Preview    Preview
	ToolCallID string
	ApprovalID string
	Prompts    []ApprovalPrompt
}

type Status struct {
	State        string `json:"state"`
	FinishReason string `json:"finishReason,omitempty"`
	Terminal     any    `json:"terminal"`
	Error        any    `json:"error"`
}

type Preview struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

type UIMessageMetadata struct {
	ThreadID string      `json:"threadId"`
	RunID    string      `json:"runId"`
	Status   Status      `json:"status"`
	Usage    *agui.Usage `json:"usage,omitempty"`
}

func (m UIMessageMetadata) Map() map[string]any {
	out := map[string]any{
		"threadId": m.ThreadID,
		"runId":    m.RunID,
		"status":   m.Status,
	}
	if m.Usage != nil {
		out["usage"] = *m.Usage
	}
	return out
}

type RunMetadata struct {
	Schema    string
	Protocol  string
	ThreadID  string
	RunID     string
	MessageID string
	AgentID   string
	AgentName string
	Model     string
	Usage     agui.Usage
	Status    Status
	Approvals []ApprovalSummary
	Artifacts ArtifactSummary
	Data      map[string]any
	Preview   Preview
}

func (m RunMetadata) Map() map[string]any {
	return map[string]any{
		"schema":    m.Schema,
		"protocol":  m.Protocol,
		"threadId":  m.ThreadID,
		"runId":     m.RunID,
		"messageId": m.MessageID,
		"agent": map[string]any{
			"id":          m.AgentID,
			"displayName": m.AgentName,
		},
		"model": m.Model,
		"usage": map[string]any{
			"promptTokens":     m.Usage.PromptTokens,
			"completionTokens": m.Usage.CompletionTokens,
			"totalTokens":      m.Usage.TotalTokens,
		},
		"usageDetails": map[string]any{},
		"status":       m.Status,
		"approvals":    m.Approvals,
		"artifacts":    m.Artifacts,
		"data":         m.Data,
		"preview":      m.Preview,
	}
}

type ApprovalSummary struct {
	ID         string         `json:"id"`
	ToolCallID string         `json:"toolCallId"`
	State      string         `json:"state"`
	Always     bool           `json:"always"`
	Reason     string         `json:"reason,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ApprovalPrompt struct {
	ID         string
	ToolCallID string
	ToolName   string
	SeqStart   int
}

type ArtifactSummary struct {
	Sources   []map[string]any `json:"sources"`
	Documents []map[string]any `json:"documents"`
	Files     []map[string]any `json:"files"`
}

type Writer struct {
	Run           *Run
	builder       agui.EventBuilder
	reasoningOpen bool
}

func NewRun(runID, threadID, model, agentID, agentName string, now time.Time) *Run {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		runID = fmt.Sprintf("run-%d", now.UnixNano())
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = runID
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	if agentID == "" {
		agentID = "ai"
	}
	if agentName == "" {
		agentName = "AI"
	}
	run := &Run{
		ThreadID:  threadID,
		RunID:     runID,
		MessageID: "msg-" + runID,
		Model:     model,
		AgentID:   agentID,
		AgentName: agentName,
		Data:      map[string]any{},
		Status:    Status{State: "streaming"},
	}
	run.Preview = Preview{Text: BoundedPreview("", PreviewBudgetBytes)}
	return run
}

func NewWriter(run *Run, now func() time.Time) *Writer {
	return &Writer{Run: run, builder: agui.NewEventBuilder(run.Model, now)}
}

func (w *Writer) Add(evt agui.Event) {
	if w == nil || w.Run == nil || len(evt) == 0 {
		return
	}
	w.Run.Events = append(w.Run.Events, evt)
	w.applySummary(evt)
}

func (w *Writer) Start() {
	w.Add(w.builder.RunStarted(w.Run.ThreadID, w.Run.RunID))
	w.Add(w.builder.TextMessageStart(w.Run.MessageID, agui.RoleAssistant))
}

func (w *Writer) Text(delta string) {
	if delta == "" {
		return
	}
	w.Add(w.builder.TextMessageContent(w.Run.MessageID, delta))
}

func (w *Writer) Thinking(delta string) {
	if delta == "" {
		return
	}
	if !w.reasoningOpen {
		w.Add(w.builder.ReasoningStart(w.Run.MessageID))
		w.Add(w.builder.ReasoningMessageStart(w.Run.MessageID))
		w.reasoningOpen = true
	}
	w.Add(w.builder.ReasoningMessageContent(w.Run.MessageID, delta))
}

func (w *Writer) StepStart(stepID string) {
	w.Add(w.builder.StepStarted(w.Run.MessageID, stepID))
}

func (w *Writer) StepFinish(stepID string) {
	w.Add(w.builder.StepFinished(w.Run.MessageID, stepID))
}

func (w *Writer) ToolStart(toolCallID, name string, index int, approval *agui.ToolApproval) {
	w.ToolStartWithMetadata(toolCallID, name, index, approval, nil)
}

func (w *Writer) ToolStartWithMetadata(toolCallID, name string, index int, approval *agui.ToolApproval, metadata map[string]any) {
	idx := index
	w.Add(w.builder.ToolCallStartWithMetadata(w.Run.MessageID, toolCallID, name, &idx, approval, metadata))
	if approval != nil {
		w.recordApprovalRequest(toolCallID, name, approval)
	}
}

func (w *Writer) ToolApprovalRequested(toolCallID, name string, input any, approval agui.ToolApproval) {
	w.ToolApprovalRequestedWithMetadata(toolCallID, name, input, approval, nil)
}

func (w *Writer) ToolApprovalRequestedWithMetadata(toolCallID, name string, input any, approval agui.ToolApproval, metadata map[string]any) {
	w.recordApprovalRequest(toolCallID, name, &approval)
	value := NewApprovalRequestedValue(*w.Run, toolCallID, name, input, approval)
	value.Metadata = metadata
	w.Add(w.builder.Custom(
		agui.ApprovalCustomRequested,
		value.Map(),
	))
}

func (w *Writer) recordApprovalRequest(toolCallID, name string, approval *agui.ToolApproval) {
	if approval == nil || approval.ID == "" {
		return
	}
	w.Run.ToolCallID = toolCallID
	w.Run.ApprovalID = approval.ID
	for _, existing := range w.Run.Approvals {
		if existing.ID == approval.ID {
			return
		}
	}
	w.Run.Approvals = append(w.Run.Approvals, ApprovalSummary{
		ID:         approval.ID,
		ToolCallID: toolCallID,
		State:      "requested",
	})
	w.Run.Prompts = append(w.Run.Prompts, ApprovalPrompt{ID: approval.ID, ToolCallID: toolCallID, ToolName: name})
}

func (w *Writer) ToolArgs(toolCallID, delta string, args any) {
	w.Add(w.builder.ToolCallArgs(toolCallID, delta, args))
}

func (w *Writer) ToolEnd(toolCallID, name string, input, result any) {
	if result == nil {
		result = map[string]any{
			"state":  agui.ToolResultStateComplete,
			"status": "success",
		}
	}
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(result), agui.ToolStateInputComplete))
}

func (w *Writer) ToolApprovalInputComplete(toolCallID, name string, input any) {
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, nil, agui.ToolStateApprovalRequested))
}

func (w *Writer) ToolApprovalResponded(toolCallID, name string, input any, response agui.ToolApprovalResponse) {
	for i := range w.Run.Approvals {
		if w.Run.Approvals[i].ID == response.ID {
			w.Run.Approvals[i].State = approvalSummaryState(response)
			w.Run.Approvals[i].Always = response.Always
			w.Run.Approvals[i].Reason = response.Reason
			w.Run.Approvals[i].Fields = response.Fields
			w.Run.Approvals[i].Metadata = response.Metadata
		}
	}
	w.Add(w.builder.Custom(agui.ApprovalCustomResponded, map[string]any{
		"threadId":   w.Run.ThreadID,
		"runId":      w.Run.RunID,
		"messageId":  w.Run.MessageID,
		"toolCallId": toolCallID,
		"toolName":   name,
		"approval":   response,
	}))
	result := map[string]any{
		"approvalId": response.ID,
		"always":     response.Always,
	}
	if response.Fields != nil {
		result["fields"] = response.Fields
	}
	if response.Metadata != nil {
		result["metadata"] = response.Metadata
	}
	if response.Approved {
		result["state"] = agui.ToolResultStateComplete
		result["status"] = "success"
		result["approved"] = true
	} else {
		reason := response.Reason
		if reason == "" {
			reason = "denied"
		}
		result["state"] = agui.ToolResultStateError
		result["status"] = "denied"
		result["reason"] = reason
	}
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(result), agui.ToolStateApprovalResponded))
}

func (w *Writer) ToolResult(toolCallID, content, state string) {
	w.Add(w.builder.ToolCallResult(w.Run.MessageID, toolCallID, content, state, agui.RoleTool))
}

func (w *Writer) ToolError(toolCallID, name string, input any, reason string) {
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(map[string]any{
		"state":  agui.ToolResultStateError,
		"status": "failed",
		"reason": reason,
	}), agui.ToolStateInputComplete))
}

func (w *Writer) ToolDenied(toolCallID, name string, input any, approvalID, reason string) {
	if reason == "" {
		reason = "denied"
	}
	for i := range w.Run.Approvals {
		if w.Run.Approvals[i].ID == approvalID {
			w.Run.Approvals[i].State = "denied"
			w.Run.Approvals[i].Reason = reason
		}
	}
	w.Add(w.builder.Custom(agui.ApprovalCustomResponded, map[string]any{
		"approval": agui.ToolApprovalResponse{ID: approvalID, Approved: false, Reason: reason},
	}))
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(map[string]any{
		"state":  agui.ToolResultStateError,
		"status": "denied",
		"reason": reason,
	}), agui.ToolStateApprovalResponded))
}

func (w *Writer) StateSnapshot(state map[string]any) {
	w.Add(w.builder.StateSnapshot(state))
}

func (w *Writer) StateDelta(delta any) {
	w.Add(w.builder.StateDelta(delta))
}

func (w *Writer) MessagesSnapshot(messages []agui.UIMessage) {
	w.Add(w.builder.MessagesSnapshot(messages))
}

func (w *Writer) Custom(name string, value any) {
	w.Add(w.builder.Custom(name, value))
}

func (w *Writer) Finish(reason string) {
	reason = agui.NormalizeFinishReason(reason)
	text := w.Run.Text()
	w.finishReasoning()
	w.Run.Usage = agui.Usage{
		PromptTokens:     1,
		CompletionTokens: utf8.RuneCountInString(text),
		TotalTokens:      utf8.RuneCountInString(text) + 1,
	}
	w.Run.Status = Status{State: "complete", FinishReason: reason}
	w.Add(w.builder.TextMessageEnd(w.Run.MessageID))
	w.addFinalSnapshot()
	w.Add(w.builder.RunFinished(w.Run.ThreadID, w.Run.RunID, reason, w.Run.Usage))
}

func (w *Writer) Error(message string) {
	w.finishReasoning()
	w.Run.Status = Status{State: "error", Error: map[string]any{"message": message}}
	w.addFinalSnapshot()
	w.Add(w.builder.RunError(w.Run.ThreadID, w.Run.RunID, message))
}

func (w *Writer) Abort(message string) {
	w.finishReasoning()
	w.Run.Status = Status{State: "aborted", Error: map[string]any{"message": message}}
	w.addFinalSnapshot()
	w.Add(w.builder.RunError(w.Run.ThreadID, w.Run.RunID, message))
}

func (w *Writer) addFinalSnapshot() {
	if w == nil || w.Run == nil {
		return
	}
	w.MessagesSnapshot([]agui.UIMessage{w.Run.FinalUIMessage(0, true)})
}

func (w *Writer) finishReasoning() {
	if !w.reasoningOpen {
		return
	}
	w.Add(w.builder.ReasoningMessageEnd(w.Run.MessageID))
	w.Add(w.builder.ReasoningEnd(w.Run.MessageID))
	w.reasoningOpen = false
}

func (w *Writer) applySummary(evt agui.Event) {
	switch evt["type"] {
	case agui.EventTextMessageContent:
		if delta, _ := evt["delta"].(string); delta != "" {
			w.Run.Preview = PreviewFromText(w.Run.Text(), PreviewBudgetBytes)
		}
	case agui.EventCustom:
		name, _ := evt["name"].(string)
		value, _ := evt["value"].(map[string]any)
		switch name {
		case "com.beeper.source":
			w.Run.Artifacts.Sources = append(w.Run.Artifacts.Sources, value)
		case "com.beeper.document":
			w.Run.Artifacts.Documents = append(w.Run.Artifacts.Documents, value)
		case "com.beeper.file":
			w.Run.Artifacts.Files = append(w.Run.Artifacts.Files, value)
		case "com.beeper.data":
			if key, _ := value["name"].(string); key != "" {
				w.Run.Data[key] = value["value"]
			}
		}
	}
}
