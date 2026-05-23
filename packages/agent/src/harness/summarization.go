package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agent "github.com/earendil-works/pi-mono/packages/agent/src"
	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

const SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const summarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const turnPrefixSummarizationPrompt = `This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.

Summarize the prefix to provide context for the retained suffix:

## Original Request
[What did the user ask for in this turn?]

## Early Progress
- [Key decisions and work done in the prefix]

## Context for Suffix
- [Information needed to understand the retained recent work]

Be concise. Focus on what's needed to understand the kept suffix.`

const branchSummaryPreamble = `The user explored a different conversation branch before returning here.
Summary of that exploration:

`

const branchSummaryPrompt = `Create a structured summary of this conversation branch for context when returning later.

Use this EXACT format:

## Goal
[What was the user trying to accomplish in this branch?]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Work that was started but not finished]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [What should happen next to continue this work]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

type SummaryGenerationOptions struct {
	Model              ai.Model
	APIKey             string
	Headers            map[string]string
	StreamFn           agent.StreamFn
	CustomInstructions string
	ThinkingLevel      agent.ThinkingLevel
}

func GenerateSummary(ctx context.Context, preparation CompactionPreparation, options SummaryGenerationOptions) (CompactResult, error) {
	if preparation.FirstKeptEntryID == "" {
		return CompactResult{}, errors.New("First kept entry has no UUID - session may need migration")
	}
	var summary string
	var err error
	if preparation.IsSplitTurn && len(preparation.TurnPrefixMessages) > 0 {
		historySummary := "No prior history."
		if len(preparation.MessagesToSummarize) > 0 {
			historySummary, err = generateSummaryText(ctx, preparation.MessagesToSummarize, preparation.PreviousSummary, preparation.Settings.ReserveTokens, options)
			if err != nil {
				return CompactResult{}, err
			}
		}
		turnPrefixSummary, err := generateTurnPrefixSummary(ctx, preparation.TurnPrefixMessages, preparation.Settings.ReserveTokens, options)
		if err != nil {
			return CompactResult{}, err
		}
		summary = historySummary + "\n\n---\n\n**Turn Context (split turn):**\n\n" + turnPrefixSummary
	} else {
		summary, err = generateSummaryText(ctx, preparation.MessagesToSummarize, preparation.PreviousSummary, preparation.Settings.ReserveTokens, options)
		if err != nil {
			return CompactResult{}, err
		}
	}
	readFiles, modifiedFiles := ComputeFileLists(preparation.FileOps)
	summary += FormatFileOperations(readFiles, modifiedFiles)
	return CompactResult{Summary: summary, FirstKeptEntryID: preparation.FirstKeptEntryID, TokensBefore: preparation.TokensBefore, Details: CompactionDetails{ReadFiles: readFiles, ModifiedFiles: modifiedFiles}}, nil
}

func GenerateBranchSummary(ctx context.Context, preparation BranchPreparation, options SummaryGenerationOptions) (BranchSummaryResult, error) {
	if len(preparation.Messages) == 0 {
		return BranchSummaryResult{Summary: "No content to summarize"}, nil
	}
	llmMessages := ConvertToLlm(preparation.Messages)
	conversationText := SerializeConversation(llmMessages)
	instructions := branchSummaryPrompt
	if options.CustomInstructions != "" {
		instructions += "\n\nAdditional focus: " + options.CustomInstructions
	}
	promptText := "<conversation>\n" + conversationText + "\n</conversation>\n\n" + instructions
	response, err := completeSimple(ctx, options.StreamFn, options.Model, ai.Context{
		SystemPrompt: SummarizationSystemPrompt,
		Messages:     []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: promptText}}, Timestamp: time.Now().UnixMilli()}},
	}, simpleOptions(options, 2048))
	if err != nil {
		return BranchSummaryResult{}, err
	}
	if response.StopReason == ai.StopReasonAborted {
		return BranchSummaryResult{}, errors.New(nonEmpty(response.ErrorMessage, "Branch summary aborted"))
	}
	if response.StopReason == ai.StopReasonError {
		return BranchSummaryResult{}, fmt.Errorf("Branch summary failed: %s", nonEmpty(response.ErrorMessage, "Unknown error"))
	}
	summary := branchSummaryPreamble + assistantText(response)
	readFiles, modifiedFiles := ComputeFileLists(preparation.FileOps)
	summary += FormatFileOperations(readFiles, modifiedFiles)
	return BranchSummaryResult{Summary: nonEmpty(summary, "No summary generated"), ReadFiles: readFiles, ModifiedFiles: modifiedFiles}, nil
}

func generateSummaryText(ctx context.Context, messages []agent.AgentMessage, previousSummary string, reserveTokens int, options SummaryGenerationOptions) (string, error) {
	basePrompt := summarizationPrompt
	if previousSummary != "" {
		basePrompt = updateSummarizationPrompt
	}
	if options.CustomInstructions != "" {
		basePrompt += "\n\nAdditional focus: " + options.CustomInstructions
	}
	conversationText := SerializeConversation(ConvertToLlm(messages))
	promptText := "<conversation>\n" + conversationText + "\n</conversation>\n\n"
	if previousSummary != "" {
		promptText += "<previous-summary>\n" + previousSummary + "\n</previous-summary>\n\n"
	}
	promptText += basePrompt
	response, err := completeSimple(ctx, options.StreamFn, options.Model, ai.Context{
		SystemPrompt: SummarizationSystemPrompt,
		Messages:     []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: promptText}}, Timestamp: time.Now().UnixMilli()}},
	}, simpleOptions(options, summaryMaxTokens(options.Model, reserveTokens, 0.8)))
	if err != nil {
		return "", err
	}
	if response.StopReason == ai.StopReasonAborted {
		return "", errors.New(nonEmpty(response.ErrorMessage, "Summarization aborted"))
	}
	if response.StopReason == ai.StopReasonError {
		return "", fmt.Errorf("Summarization failed: %s", nonEmpty(response.ErrorMessage, "Unknown error"))
	}
	return assistantText(response), nil
}

func generateTurnPrefixSummary(ctx context.Context, messages []agent.AgentMessage, reserveTokens int, options SummaryGenerationOptions) (string, error) {
	promptText := "<conversation>\n" + SerializeConversation(ConvertToLlm(messages)) + "\n</conversation>\n\n" + turnPrefixSummarizationPrompt
	response, err := completeSimple(ctx, options.StreamFn, options.Model, ai.Context{
		SystemPrompt: SummarizationSystemPrompt,
		Messages:     []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: promptText}}, Timestamp: time.Now().UnixMilli()}},
	}, simpleOptions(options, summaryMaxTokens(options.Model, reserveTokens, 0.5)))
	if err != nil {
		return "", err
	}
	if response.StopReason == ai.StopReasonAborted {
		return "", errors.New(nonEmpty(response.ErrorMessage, "Turn prefix summarization aborted"))
	}
	if response.StopReason == ai.StopReasonError {
		return "", fmt.Errorf("Turn prefix summarization failed: %s", nonEmpty(response.ErrorMessage, "Unknown error"))
	}
	return assistantText(response), nil
}

func completeSimple(ctx context.Context, streamFn agent.StreamFn, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) (ai.Message, error) {
	if streamFn == nil {
		return ai.Message{}, errors.New("stream function is required")
	}
	stream := streamFn(ctx, model, llmContext, options)
	return stream.Result(), nil
}

func simpleOptions(options SummaryGenerationOptions, maxTokens int) ai.SimpleStreamOptions {
	streamOptions := ai.StreamOptions{APIKey: options.APIKey, Headers: options.Headers}
	if maxTokens > 0 {
		streamOptions.MaxTokens = &maxTokens
	}
	simple := ai.SimpleStreamOptions{StreamOptions: streamOptions}
	if options.Model.Reasoning && options.ThinkingLevel != "" && options.ThinkingLevel != agent.ThinkingLevelOff {
		level := ai.ThinkingLevel(options.ThinkingLevel)
		simple.Reasoning = &level
	}
	return simple
}

func summaryMaxTokens(model ai.Model, reserveTokens int, fraction float64) int {
	maxTokens := int(float64(reserveTokens) * fraction)
	if model.MaxTokens > 0 && model.MaxTokens < maxTokens {
		return model.MaxTokens
	}
	return maxTokens
}

func assistantText(message ai.Message) string {
	parts := []string{}
	for _, block := range contentBlocks(message.Content) {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func nonEmpty(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
