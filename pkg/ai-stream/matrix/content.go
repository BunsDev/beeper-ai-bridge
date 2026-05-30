package matrix

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/beeper/ai-bridge/pkg/ai-stream"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

const ApprovalRelationType = event.RelationType("com.beeper.ai.approval")
const finalSegmentSizeTargetEventID = id.EventID("$0000000000000000000000000000000000000000")

type FinalProjection struct {
	Content  *event.MessageEventContent
	Extra    map[string]any
	Segments []aistream.FinalSegment
}

func AnchorContent(run aistream.Run) (*event.MessageEventContent, map[string]any) {
	content := previewContent(run)
	extra := map[string]any{
		aistream.BeeperAIKey: run.AIWithMessage(aistream.AIKindAnchor, run.InitialBeeperAIMessage()),
	}
	return content, extra
}

func ProjectFinal(run aistream.Run) FinalProjection {
	uiMessage, segments, content := projectFinal(run, aistream.FinalMessageBudgetBytes)
	extra := map[string]any{
		aistream.BeeperAIKey: finalAIContent(run, uiMessage, len(segments)),
	}
	return FinalProjection{Content: content, Extra: extra, Segments: segments}
}

func FinalContent(run aistream.Run) (*event.MessageEventContent, map[string]any) {
	projection := ProjectFinal(run)
	return projection.Content, projection.Extra
}

func finalAIContent(run aistream.Run, message aistream.UIMessage, segmentCount int) aistream.BeeperAI {
	if segmentCount > 0 {
		run.Final = aistream.FinalDelivery{Delivery: "segmented", SegmentCount: segmentCount}
	} else {
		run.Final = aistream.FinalDelivery{Delivery: "inline", SegmentCount: 0}
	}
	return run.AIWithMessage(aistream.AIKindFinal, message)
}

func FinalSegments(run aistream.Run) []aistream.FinalSegment {
	return ProjectFinal(run).Segments
}

func projectFinal(run aistream.Run, budget int) (aistream.UIMessage, []aistream.FinalSegment, *event.MessageEventContent) {
	if budget <= 0 {
		budget = aistream.FinalMessageBudgetBytes
	}
	fullMessage := run.FinalBeeperAIMessage(0, true)
	anchorMessage := aistream.UIMessage{ID: fullMessage.ID, Role: fullMessage.Role, Parts: []aistream.MessagePart{}}
	partSegments := aistream.FinalUIPartSegments(run, budget)
	text := finalVisibleText(run)
	countHint := len(partSegments)
	var content *event.MessageEventContent
	var segments []aistream.FinalSegment
	for attempt := 0; attempt < 4; attempt++ {
		content, segments = projectFinalWithCount(run, anchorMessage, partSegments, text, countHint, budget)
		if len(segments) == countHint {
			return anchorMessage, segments, content
		}
		countHint = len(segments)
	}
	content, segments = projectFinalWithCount(run, anchorMessage, partSegments, text, countHint, budget)
	return anchorMessage, segments, content
}

func projectFinalWithCount(run aistream.Run, anchorMessage aistream.UIMessage, partSegments []aistream.FinalSegment, text string, countHint int, budget int) (*event.MessageEventContent, []aistream.FinalSegment) {
	anchorExtra := map[string]any{
		aistream.BeeperAIKey: finalAIContentWithSegmentCount(run, anchorMessage, countHint),
	}
	anchorContent, _, remainingText := fitFinalTextContent(run, text, budget, func(content *event.MessageEventContent) int {
		return finalPayloadSize(content, anchorExtra)
	})
	partParts := flattenFinalSegmentParts(partSegments)
	segments := make([]aistream.FinalSegment, 0, countHint)
	partIndex := 0
	segmentIndex := 0
	for remainingText != "" {
		segment := aistream.FinalSegment{
			Message: aistream.UIMessage{ID: anchorMessage.ID, Role: anchorMessage.Role, Parts: []aistream.MessagePart{}},
			Metadata: aistream.FinalSegmentMetadata{
				RunID:     run.RunID,
				MessageID: run.MessageID,
				Index:     segmentIndex,
				Count:     countHint,
			},
		}
		segment.Text, remainingText = fitFinalSegmentText(run, segment, remainingText, budget)
		for partIndex < len(partParts) {
			candidate := append(append([]aistream.MessagePart(nil), segment.Message.Parts...), partParts[partIndex])
			candidateSegment := segment
			candidateSegment.Message.Parts = candidate
			if finalSegmentProjectionSize(run, candidateSegment) > budget {
				break
			}
			segment.Message.Parts = candidate
			partIndex++
		}
		segments = append(segments, segment)
		segmentIndex++
	}
	for partIndex < len(partParts) {
		segment := aistream.FinalSegment{
			Message: aistream.UIMessage{ID: anchorMessage.ID, Role: anchorMessage.Role, Parts: []aistream.MessagePart{}},
			Metadata: aistream.FinalSegmentMetadata{
				RunID:     run.RunID,
				MessageID: run.MessageID,
				Index:     segmentIndex,
				Count:     countHint,
			},
		}
		for partIndex < len(partParts) {
			candidate := append(append([]aistream.MessagePart(nil), segment.Message.Parts...), partParts[partIndex])
			candidateSegment := segment
			candidateSegment.Message.Parts = candidate
			if len(segment.Message.Parts) > 0 && finalSegmentProjectionSize(run, candidateSegment) > budget {
				break
			}
			segment.Message.Parts = candidate
			partIndex++
			if finalSegmentProjectionSize(run, segment) > budget {
				break
			}
		}
		segments = append(segments, segment)
		segmentIndex++
	}
	for index := range segments {
		segments[index].Metadata.Index = index
		segments[index].Metadata.Count = len(segments)
	}
	return anchorContent, segments
}

func finalAIContentWithSegmentCount(run aistream.Run, message aistream.UIMessage, segmentCount int) aistream.BeeperAI {
	if segmentCount > 0 {
		run.Final = aistream.FinalDelivery{Delivery: "segmented", SegmentCount: segmentCount}
	} else {
		run.Final = aistream.FinalDelivery{Delivery: "inline", SegmentCount: 0}
	}
	return run.AIWithMessage(aistream.AIKindFinal, message)
}

func flattenFinalSegmentParts(segments []aistream.FinalSegment) []aistream.MessagePart {
	var parts []aistream.MessagePart
	for _, segment := range segments {
		parts = append(parts, segment.Message.Parts...)
	}
	return parts
}

func finalVisibleText(run aistream.Run) string {
	if text := run.Text(); text != "" {
		return text
	}
	if run.Preview.Text != "" {
		return run.Preview.Text
	}
	return "..."
}

func fitFinalSegmentText(run aistream.Run, segment aistream.FinalSegment, text string, budget int) (string, string) {
	_, chunk, rest := fitFinalTextContent(run, text, budget, func(content *event.MessageEventContent) int {
		return finalSegmentProjectionSizeWithContent(run, segment, content)
	})
	return chunk, rest
}

func fitFinalTextContent(run aistream.Run, text string, budget int, size func(*event.MessageEventContent) int) (*event.MessageEventContent, string, string) {
	if text == "" {
		return finalTextContent(run, "", false), "", ""
	}
	type candidate struct {
		text        string
		content     *event.MessageEventContent
		includeBody bool
	}
	best := candidate{}
	for _, includeBody := range []bool{false, true} {
		low, high := 0, len(text)
		var local candidate
		for low <= high {
			mid := (low + high) / 2
			prefix := utf8Prefix(text, mid)
			if prefix == "" {
				low = mid + 1
				continue
			}
			content := finalTextContent(run, prefix, includeBody)
			if size(content) <= budget {
				local = candidate{text: prefix, content: content, includeBody: includeBody}
				low = len(prefix) + 1
			} else {
				high = len(prefix) - 1
			}
		}
		if len(local.text) > len(best.text) || (len(local.text) == len(best.text) && local.includeBody) {
			best = local
		}
	}
	if best.content == nil {
		_, size := utf8.DecodeRuneInString(text)
		if size <= 0 {
			return finalTextContent(run, "", false), "", ""
		}
		chunk := text[:size]
		return finalTextContent(run, chunk, false), chunk, strings.TrimPrefix(text, chunk)
	}
	return best.content, best.text, strings.TrimPrefix(text, best.text)
}

func utf8Prefix(text string, maxBytes int) string {
	if maxBytes >= len(text) {
		return text
	}
	if maxBytes <= 0 {
		return ""
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

func finalTextContent(run aistream.Run, text string, includeBody bool) *event.MessageEventContent {
	rendered := format.RenderMarkdown(text, true, false)
	content := &rendered
	if !includeBody {
		content.Body = ""
	}
	content.EnsureHasHTML()
	content.BeeperPerMessageProfile = &event.BeeperPerMessageProfile{
		ID:          run.AgentID,
		Displayname: run.AgentName,
	}
	return content
}

func finalPayloadSize(content *event.MessageEventContent, extra map[string]any) int {
	return aistream.JSONSize(map[string]any{"content": content, "extra": extra})
}

func finalSegmentProjectionSize(run aistream.Run, segment aistream.FinalSegment) int {
	content, extra := FinalSegmentContent(run, segment, finalSegmentSizeTargetEventID)
	return finalPayloadSize(content, extra)
}

func finalSegmentProjectionSizeWithContent(run aistream.Run, segment aistream.FinalSegment, content *event.MessageEventContent) int {
	contentWithRelation := *content
	contentWithRelation.SetRelatesTo(&event.RelatesTo{Type: event.RelReference, EventID: finalSegmentSizeTargetEventID})
	extra := map[string]any{
		aistream.BeeperAIKey: run.AISegment(segment.Message, segment.Metadata),
	}
	return finalPayloadSize(&contentWithRelation, extra)
}

func previewContent(run aistream.Run) *event.MessageEventContent {
	body := run.Preview.Text
	if body == "" {
		body = "..."
	}
	rendered := format.RenderMarkdown(body, true, false)
	content := &rendered
	content.EnsureHasHTML()
	content.BeeperPerMessageProfile = &event.BeeperPerMessageProfile{
		ID:          run.AgentID,
		Displayname: run.AgentName,
	}
	return content
}

func CarrierContent(run aistream.Run, carrier aistream.Carrier, targetEventID id.EventID) (*event.MessageEventContent, map[string]any) {
	content := format.TextToContent("")
	content.SetRelatesTo(&event.RelatesTo{Type: event.RelReference, EventID: targetEventID})
	return &content, aistream.CarrierContent(run, carrier.Envelopes)
}

func FinalSegmentContent(run aistream.Run, segment aistream.FinalSegment, targetEventID id.EventID) (*event.MessageEventContent, map[string]any) {
	content := finalTextContent(run, segment.Text, false)
	content.SetRelatesTo(&event.RelatesTo{Type: event.RelReference, EventID: targetEventID})
	return content, map[string]any{
		aistream.BeeperAIKey: run.AISegment(segment.Message, segment.Metadata),
	}
}

func ApprovalContent(ctx aistream.ApprovalContext, choices []aistream.ApprovalChoice) (*event.MessageEventContent, map[string]any) {
	toolName := ctx.ToolName
	body := fmt.Sprintf("Approval required for %s", toolName)
	if len(choices) > 0 {
		body += "\nReact with one of the listed choices."
	}
	content := format.TextToContent(body)
	if ctx.TargetEvent != "" {
		content.SetRelatesTo(&event.RelatesTo{Type: ApprovalRelationType, EventID: id.EventID(ctx.TargetEvent)})
	}
	extra := map[string]any{
		aistream.BeeperAIApprovalKey: aistream.NewApprovalNotice(ctx, choices).Map(),
	}
	return &content, extra
}

func ApprovalChoicesAsAny(choices []aistream.ApprovalChoice) []any {
	return aistream.ApprovalChoicesAsAny(choices)
}
