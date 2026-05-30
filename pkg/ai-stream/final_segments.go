package aistream

import (
	"encoding/json"
	"fmt"
)

const FinalMessageBudgetBytes = 64 * 1024
const finalPartFragmentType = "data-com-beeper-final-part-fragment"

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

func FinalUIMessageContent(run Run, budget int) (UIMessage, []FinalSegment) {
	if budget <= 0 {
		budget = FinalMessageBudgetBytes
	}
	message := run.FinalBeeperAIMessage(0, true)
	inlineRun := run
	inlineRun.Final = FinalDelivery{Delivery: "inline", SegmentCount: 0}
	if JSONSize(map[string]any{BeeperAIKey: inlineRun.AIWithMessage(AIKindFinal, message)}) <= budget {
		return message, nil
	}

	segments := packFinalSegments(run, message, message.Parts, budget)
	assignFinalSegmentMetadata(run, segments)
	return UIMessage{ID: message.ID, Role: message.Role, Parts: []MessagePart{}}, segments
}

func FinalUIPartSegments(run Run, budget int) []FinalSegment {
	if budget <= 0 {
		budget = FinalMessageBudgetBytes
	}
	message := run.FinalBeeperAIMessage(0, true)
	if len(message.Parts) == 0 {
		return nil
	}
	segments := packFinalSegments(run, message, message.Parts, budget)
	assignFinalSegmentMetadata(run, segments)
	return segments
}

func packFinalSegments(run Run, message UIMessage, parts []MessagePart, budget int) []FinalSegment {
	if len(parts) == 0 {
		return []FinalSegment{{Message: UIMessage{ID: message.ID, Role: message.Role, Parts: []MessagePart{}}}}
	}
	splitParts := splitFinalPartsForPayload(run, message, parts, budget)
	maxMetadata := FinalSegmentMetadata{RunID: run.RunID, MessageID: run.MessageID, Index: len(splitParts), Count: len(splitParts)}
	segments := make([]FinalSegment, 0)
	current := make([]MessagePart, 0)
	for _, part := range splitParts {
		candidate := append(append([]MessagePart(nil), current...), part)
		if len(current) > 0 && finalSegmentPayloadSize(run, message, candidate, maxMetadata) > budget {
			segments = append(segments, FinalSegment{Message: finalSegmentMessage(message, current)})
			current = []MessagePart{part}
			continue
		}
		current = candidate
	}
	if len(current) > 0 {
		segments = append(segments, FinalSegment{Message: finalSegmentMessage(message, current)})
	}
	return segments
}

func splitFinalPartsForPayload(run Run, message UIMessage, parts []MessagePart, budget int) []MessagePart {
	metadata := FinalSegmentMetadata{RunID: run.RunID, MessageID: run.MessageID, Index: len(parts), Count: len(parts)}
	out := parts
	for attempt := 0; attempt < 4; attempt++ {
		next := make([]MessagePart, 0, len(out))
		for index, part := range parts {
			next = append(next, splitFinalPartForPayload(run, message, part, index, budget, metadata)...)
		}
		out = next
		if len(out) == metadata.Count {
			break
		}
		metadata.Index = len(out)
		metadata.Count = len(out)
	}
	return out
}

func splitFinalPartForPayload(run Run, message UIMessage, part MessagePart, partIndex int, budget int, metadata FinalSegmentMetadata) []MessagePart {
	if finalSegmentPayloadSize(run, message, []MessagePart{part}, metadata) <= budget {
		return []MessagePart{part}
	}
	content, _ := part["content"].(string)
	if content != "" {
		maxContentBytes := len(content) / 2
		if maxContentBytes > budget/2 {
			maxContentBytes = budget / 2
		}
		if maxContentBytes < 1 {
			maxContentBytes = 1
		}
		for maxContentBytes >= 1 {
			chunks := SplitTextUTF8(content, maxContentBytes)
			out := make([]MessagePart, 0, len(chunks))
			allFit := true
			for _, chunk := range chunks {
				split := cloneValueMap(part)
				split["content"] = chunk
				if finalSegmentPayloadSize(run, message, []MessagePart{split}, metadata) > budget {
					allFit = false
					break
				}
				out = append(out, split)
			}
			if allFit {
				return out
			}
			maxContentBytes /= 2
		}
	}
	return splitFinalPartAsFragments(run, message, part, partIndex, budget, metadata)
}

func splitFinalPartAsFragments(run Run, message UIMessage, part MessagePart, partIndex int, budget int, metadata FinalSegmentMetadata) []MessagePart {
	encoded, err := json.Marshal(part)
	if err != nil {
		return []MessagePart{part}
	}
	maxDataBytes := len(encoded) / 2
	if maxDataBytes > budget/2 {
		maxDataBytes = budget / 2
	}
	if maxDataBytes < 1 {
		maxDataBytes = 1
	}
	for maxDataBytes >= 1 {
		chunks := SplitTextUTF8(string(encoded), maxDataBytes)
		out := make([]MessagePart, 0, len(chunks))
		allFit := true
		for index, chunk := range chunks {
			fragment := finalPartFragment(part, partIndex, index, len(chunks), chunk)
			if finalSegmentPayloadSize(run, message, []MessagePart{fragment}, metadata) > budget {
				allFit = false
				break
			}
			out = append(out, fragment)
		}
		if allFit {
			return out
		}
		maxDataBytes /= 2
	}
	return []MessagePart{part}
}

func finalPartFragment(part MessagePart, partIndex int, fragmentIndex int, fragmentCount int, data string) MessagePart {
	partID := firstString(part["id"], part["toolCallId"], fmt.Sprintf("part-%d", partIndex))
	return MessagePart{
		"type":          finalPartFragmentType,
		"id":            fmt.Sprintf("%s:final-fragment:%d", partID, fragmentIndex),
		"partId":        partID,
		"partIndex":     partIndex,
		"fragmentIndex": fragmentIndex,
		"fragmentCount": fragmentCount,
		"data":          data,
	}
}

func assignFinalSegmentMetadata(run Run, segments []FinalSegment) {
	for index := range segments {
		segments[index].Metadata = FinalSegmentMetadata{
			RunID:     run.RunID,
			MessageID: run.MessageID,
			Index:     index,
			Count:     len(segments),
		}
	}
}

func finalSegmentPayloadSize(run Run, message UIMessage, parts []MessagePart, metadata FinalSegmentMetadata) int {
	segmentMessage := finalSegmentMessage(message, parts)
	return JSONSize(map[string]any{BeeperAIKey: run.AISegment(segmentMessage, metadata)})
}

func finalSegmentMessage(message UIMessage, parts []MessagePart) UIMessage {
	return UIMessage{
		ID:    message.ID,
		Role:  message.Role,
		Parts: append([]MessagePart(nil), parts...),
	}
}

func FinalSegmentTxnID(runID string, index int) string {
	if runID == "" {
		return fmt.Sprintf("ai_final_segment_%d", index)
	}
	return fmt.Sprintf("ai_final_segment_%s_%d", runID, index)
}
