package aistream

// Matrix encrypted event content expands the cleartext message, so keep the
// cleartext final projection low enough to land near a 60 KiB encrypted event.
const FinalMessageBudgetBytes = 45 * 1024

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
