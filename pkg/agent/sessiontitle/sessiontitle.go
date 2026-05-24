package sessiontitle

import (
	"context"
	"strings"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

const SystemPrompt = `You generate concise conversation titles.

Return only the title. Do not use quotes. Do not end with punctuation.`

const titlePrompt = `Create a short title for this conversation.

Rules:
- 3 to 7 words
- specific to the user's task
- no trailing punctuation
- no quotes

Conversation:
`

type Options struct {
	Model    ai.Model
	APIKey   string
	Headers  map[string]string
	StreamFn agent.StreamFn
}

func Generate(ctx context.Context, messages []agent.AgentMessage, options Options) (string, error) {
	if options.StreamFn == nil {
		options.StreamFn = ai.StreamSimple
	}
	prompt := titlePrompt + conversationText(messages)
	maxTokens := 32
	stream := options.StreamFn(ctx, options.Model, ai.Context{
		SystemPrompt: SystemPrompt,
		Messages: []ai.Message{{
			Role:    "user",
			Content: []ai.ContentBlock{{Type: "text", Text: prompt}},
		}},
	}, ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{
		APIKey:    options.APIKey,
		Headers:   options.Headers,
		MaxTokens: &maxTokens,
	}})
	return cleanTitle(assistantText(stream.Result())), nil
}

func conversationText(messages []agent.AgentMessage) string {
	var out strings.Builder
	limit := len(messages)
	if limit > 6 {
		limit = 6
	}
	for i := 0; i < limit; i++ {
		message := messages[i]
		switch message.Role {
		case "user":
			out.WriteString("\nUser: ")
		case "assistant":
			out.WriteString("\nAssistant: ")
		default:
			continue
		}
		out.WriteString(assistantText(ai.Message(message)))
	}
	return out.String()
}

func assistantText(message ai.Message) string {
	parts := []string{}
	for _, block := range contentBlocks(message.Content) {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func contentBlocks(content any) []ai.ContentBlock {
	if blocks, ok := content.([]ai.ContentBlock); ok {
		return blocks
	}
	if text, ok := content.(string); ok {
		return []ai.ContentBlock{{Type: "text", Text: text}}
	}
	return nil
}

func cleanTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'`")
	title = strings.TrimRight(title, ".!?")
	fields := strings.Fields(title)
	if len(fields) > 7 {
		fields = fields[:7]
	}
	return strings.Join(fields, " ")
}
