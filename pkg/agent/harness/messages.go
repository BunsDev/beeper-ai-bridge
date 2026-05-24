package harness

import (
	"fmt"
	"strings"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

const CompactionSummaryPrefix = `The conversation history before this point was compacted into the following summary:

<summary>
`

const CompactionSummarySuffix = `
</summary>`

const BranchSummaryPrefix = `The following is a summary of a branch that this conversation came back from:

<summary>
`

const BranchSummarySuffix = `</summary>`

func BashExecutionToText(msg agent.AgentMessage) string {
	var builder strings.Builder
	builder.WriteString("Ran `")
	builder.WriteString(msg.Command)
	builder.WriteString("`\n")
	if msg.Output != "" {
		builder.WriteString("```\n")
		builder.WriteString(msg.Output)
		builder.WriteString("\n```")
	} else {
		builder.WriteString("(no output)")
	}
	if msg.Cancelled {
		builder.WriteString("\n\n(command cancelled)")
	} else if msg.ExitCode != nil && *msg.ExitCode != 0 {
		builder.WriteString(fmt.Sprintf("\n\nCommand exited with code %d", *msg.ExitCode))
	}
	if msg.Truncated && msg.FullOutputPath != "" {
		builder.WriteString("\n\n[Output truncated. Full output: ")
		builder.WriteString(msg.FullOutputPath)
		builder.WriteString("]")
	}
	return builder.String()
}

func CreateBranchSummaryMessage(summary string, fromID string, timestamp string) agent.AgentMessage {
	return agent.AgentMessage{
		Role:      "branchSummary",
		Summary:   summary,
		FromID:    fromID,
		Timestamp: parseTimestampMillis(timestamp),
	}
}

func CreateCompactionSummaryMessage(summary string, tokensBefore int, timestamp string) agent.AgentMessage {
	return agent.AgentMessage{
		Role:         "compactionSummary",
		Summary:      summary,
		TokensBefore: tokensBefore,
		Timestamp:    parseTimestampMillis(timestamp),
	}
}

func CreateCustomMessage(customType string, content any, display bool, details any, timestamp string) agent.AgentMessage {
	return agent.AgentMessage{
		Role:       "custom",
		CustomType: customType,
		Content:    content,
		Display:    display,
		Details:    details,
		Timestamp:  parseTimestampMillis(timestamp),
	}
}

func ConvertToLlm(messages []agent.AgentMessage) []ai.Message {
	out := make([]ai.Message, 0, len(messages))
	for _, msg := range messages {
		converted, ok := convertMessageToLlm(msg)
		if ok {
			out = append(out, converted)
		}
	}
	return out
}

func convertMessageToLlm(msg agent.AgentMessage) (ai.Message, bool) {
	switch msg.Role {
	case "bashExecution":
		if msg.ExcludeFromContext {
			return ai.Message{}, false
		}
		return ai.Message{
			Role:      "user",
			Content:   []ai.ContentBlock{{Type: "text", Text: BashExecutionToText(msg)}},
			Timestamp: msg.Timestamp,
		}, true
	case "custom":
		return ai.Message{
			Role:      "user",
			Content:   messageContentBlocks(msg.Content),
			Timestamp: msg.Timestamp,
		}, true
	case "branchSummary":
		return ai.Message{
			Role:      "user",
			Content:   []ai.ContentBlock{{Type: "text", Text: BranchSummaryPrefix + msg.Summary + BranchSummarySuffix}},
			Timestamp: msg.Timestamp,
		}, true
	case "compactionSummary":
		return ai.Message{
			Role:      "user",
			Content:   []ai.ContentBlock{{Type: "text", Text: CompactionSummaryPrefix + msg.Summary + CompactionSummarySuffix}},
			Timestamp: msg.Timestamp,
		}, true
	case "user", "assistant", "toolResult":
		return msg, true
	default:
		return ai.Message{}, false
	}
}

func messageContentBlocks(content any) any {
	if text, ok := content.(string); ok {
		return []ai.ContentBlock{{Type: "text", Text: text}}
	}
	return content
}

func parseTimestampMillis(value string) int64 {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}
