package aistream

// Matrix rejects event content over 64 KiB. Final projection sizes are measured
// as the estimated encrypted edit event content, with headroom.
const FinalMessageBudgetBytes = 60 * 1024

const FinalPartsPayloadSchema = "com.beeper.ai.final_parts.v1"
const FinalPartsRefSchema = "com.beeper.ai.final_parts_ref.v1"
const FinalPartsMediaType = "application/vnd.beeper.ai.final-parts+json"

type FinalSegmentMetadata struct {
	RunID     string `json:"runId"`
	MessageID string `json:"messageId"`
	Index     int    `json:"index"`
	Count     int    `json:"count"`
}

type FinalSegment struct {
	Message  UIMessage            `json:"message"`
	Metadata FinalSegmentMetadata `json:"metadata"`
	Text     string               `json:"-"`
}

func (t Run) FinalPartsPayload(message UIMessage) FinalPartsPayload {
	return FinalPartsPayload{
		Schema:    FinalPartsPayloadSchema,
		ThreadID:  t.ThreadID,
		RunID:     t.RunID,
		MessageID: t.MessageID,
		Message:   message,
	}
}
