package aistream

import (
	"fmt"

	agui "github.com/beeper/ai-bridge/pkg/ag-ui"
)

func (t Run) AI(kind string) BeeperAI {
	final := t.finalDelivery()
	payload := BeeperAI{
		Schema:    BeeperAISchema,
		Protocol:  "ag-ui",
		Kind:      kind,
		ThreadID:  t.ThreadID,
		RunID:     t.RunID,
		MessageID: t.MessageID,
		Agent:     AgentMetadata{ID: t.AgentID, DisplayName: t.AgentName},
		Model:     t.Model,
		Data:      t.Data,
	}
	if kind == AIKindFinal {
		payload.Final = &final
		payload.Events = t.finalLifecycleEvents()
	}
	return payload
}

func (t Run) AIWithMessage(kind string, message UIMessage) BeeperAI {
	payload := t.AI(kind)
	payload.Message = &message
	return payload
}

func (t Run) AIStream(envelopes []Envelope) BeeperAI {
	payload := t.AI(AIKindStream)
	payload.Events = envelopes
	return payload
}

func (t Run) finalDelivery() FinalDelivery {
	if t.Final.Delivery != "" {
		return t.Final
	}
	return FinalDelivery{Delivery: "inline", PartsComplete: true}
}

func (t Run) finalLifecycleEvents() []Envelope {
	switch t.Status.State {
	case "complete", "interrupted":
		reason := t.Status.FinishReason
		if reason == "" {
			reason = agui.FinishReasonStop
		}
		fields := map[string]any{
			"type":         agui.EventRunFinished,
			"threadId":     t.ThreadID,
			"runId":        t.RunID,
			"finishReason": reason,
			"usage":        t.Usage,
			"outcome":      terminalOutcome(t.Status, t.Interrupts),
		}
		if t.Model != "" {
			fields["model"] = t.Model
		}
		event := agui.NewEvent(fields)
		return []Envelope{{Seq: 1, Event: event}}
	case "error", "aborted":
		err := terminalError(t.Status.Error)
		message := ""
		code := ""
		if err != nil {
			message = err.Message
			code = err.Code
		}
		if message == "" {
			message = ErrorFallbackText
		}
		if t.Status.State == "aborted" && code == "" {
			code = agui.FinishReasonCancelled
		}
		fields := map[string]any{
			"type":     agui.EventRunError,
			"threadId": t.ThreadID,
			"runId":    t.RunID,
			"message":  message,
		}
		if t.Model != "" {
			fields["model"] = t.Model
		}
		if code != "" {
			fields["code"] = code
		}
		event := agui.NewEvent(fields)
		return []Envelope{{Seq: 1, Event: event}}
	}
	for i := len(t.Events) - 1; i >= 0; i-- {
		eventType := t.Events[i].Type()
		if eventType == agui.EventRunFinished || eventType == agui.EventRunError {
			return []Envelope{{Seq: 1, Event: t.Events[i]}}
		}
	}
	return nil
}

func (t Run) Validate() error {
	for i, evt := range t.Events {
		if err := agui.ValidateEvent(evt); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
	}
	return nil
}
