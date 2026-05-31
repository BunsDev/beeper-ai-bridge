package sessiontitle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

const SystemPrompt = `Generate a concise, specific title for this chat.
Capture the main topic or task from the provided message.
Rules:
- Prefer 2 to 4 words
- Use Title Case
- Be specific and descriptive
- No quotes
- No markdown
- No labels like Title:
- No trailing punctuation
- Maximum 48 characters
- Return only the title`

const maxTitleChars = 48

type Options struct {
	Model      ai.Model
	APIKey     string
	Headers    map[string]string
	CompleteFn ai.APICompleteSimpleFunction
	StreamFn   agent.StreamFn
}

func Generate(ctx context.Context, messages []agent.AgentMessage, options Options) (string, error) {
	if options.CompleteFn == nil {
		options.CompleteFn = ai.CompleteSimple
	}
	if options.StreamFn != nil {
		streamFn := options.StreamFn
		options.CompleteFn = func(ctx context.Context, model ai.Model, llmContext ai.Context, streamOptions ai.SimpleStreamOptions) ai.Message {
			return streamFn(ctx, model, llmContext, streamOptions).Result()
		}
	}
	prompt := firstUserContent(messages)
	if len(prompt) == 0 {
		return "", nil
	}
	maxTokens := 32
	result := options.CompleteFn(ctx, options.Model, ai.Context{
		SystemPrompt: SystemPrompt,
		Messages: []ai.Message{{
			Role:    "user",
			Content: prompt,
		}},
	}, ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{
		APIKey:    options.APIKey,
		Headers:   options.Headers,
		MaxTokens: &maxTokens,
	}})
	if result.StopReason == ai.StopReasonError || result.StopReason == ai.StopReasonAborted {
		if result.ErrorMessage != "" {
			return "", fmt.Errorf("title generation failed: %s", result.ErrorMessage)
		}
		return "", fmt.Errorf("title generation failed: %s", result.StopReason)
	}
	return cleanTitle(assistantText(result)), nil
}

func firstUserContent(messages []agent.AgentMessage) []ai.ContentBlock {
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		return titlePromptBlocks(contentBlocks(message.Content))
	}
	return nil
}

func titlePromptBlocks(blocks []ai.ContentBlock) []ai.ContentBlock {
	prompt := make([]ai.ContentBlock, 0, len(blocks)+1)
	text := trimPrompt(textFromBlocks(blocks))
	if text != "" {
		prompt = append(prompt, ai.ContentBlock{Type: "text", Text: text})
	}
	attachments := []string{}
	for _, block := range blocks {
		switch block.Type {
		case "image", "audio":
			attachments = append(attachments, attachmentLabel(block))
		}
	}
	if len(attachments) > 0 && text == "" {
		prompt = append(prompt, ai.ContentBlock{Type: "text", Text: "User attached: " + strings.Join(attachments, ", ")})
	}
	return prompt
}

func assistantText(message ai.Message) string {
	return strings.TrimSpace(textFromBlocks(contentBlocks(message.Content)))
}

func textFromBlocks(blocks []ai.ContentBlock) string {
	parts := []string{}
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func attachmentLabel(block ai.ContentBlock) string {
	label := strings.TrimSpace(block.Type)
	if label == "" {
		label = "attachment"
	}
	name := strings.TrimSpace(block.Name)
	if name == "" {
		return label
	}
	return label + " " + name
}

func contentBlocks(content any) []ai.ContentBlock {
	switch value := content.(type) {
	case []ai.ContentBlock:
		return value
	case []any:
		blocks := make([]ai.ContentBlock, 0, len(value))
		for _, item := range value {
			raw, _ := json.Marshal(item)
			var block ai.ContentBlock
			if json.Unmarshal(raw, &block) == nil {
				blocks = append(blocks, block)
			}
		}
		return blocks
	case string:
		return []ai.ContentBlock{{Type: "text", Text: value}}
	default:
		raw, _ := json.Marshal(value)
		var blocks []ai.ContentBlock
		_ = json.Unmarshal(raw, &blocks)
		return blocks
	}
}

func trimPrompt(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSpace(value)
	if len(value) > 4000 {
		return value[:4000]
	}
	return value
}

func cleanTitle(title string) string {
	title = trimCodeFence(title)
	if lines := strings.Split(title, "\n"); len(lines) > 1 {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				title = line
				break
			}
		}
	}
	title = trimCodeFence(title)
	title = strings.TrimSpace(title)
	title = strings.TrimPrefix(title, "- ")
	title = strings.TrimPrefix(title, "* ")
	if rest, ok := stripLabel(title); ok {
		title = rest
	}
	title = strings.Trim(title, "\"'`")
	title = strings.TrimRight(title, ".?!:;,")
	title = strings.Trim(title, "\"'`")
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) <= maxTitleChars {
		return title
	}
	title = strings.TrimSpace(string(runes[:maxTitleChars]))
	if lastSpace := strings.LastIndex(title, " "); lastSpace > 20 {
		title = title[:lastSpace]
	}
	return strings.TrimSpace(title)
}

func trimCodeFence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		value = strings.TrimPrefix(value, "```")
		value = strings.TrimLeft(value, " \t")
		if newline := strings.IndexAny(value, "\r\n"); newline >= 0 {
			firstLine := strings.TrimSpace(value[:newline])
			if firstLine != "" && !strings.Contains(firstLine, " ") {
				value = value[newline:]
			}
		}
	}
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

func stripLabel(title string) (string, bool) {
	lower := strings.ToLower(title)
	for _, label := range []string{"title:", "session name:"} {
		if strings.HasPrefix(lower, label) {
			return strings.TrimSpace(title[len(label):]), true
		}
	}
	return title, false
}
