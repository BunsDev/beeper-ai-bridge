package providers

import ai "github.com/beeper/ai-bridge/pkg/ai"

func BuildBaseOptions(_ ai.Model, options *ai.SimpleStreamOptions, apiKey string) ai.StreamOptions {
	if options == nil {
		return ai.StreamOptions{APIKey: apiKey}
	}
	base := options.StreamOptions
	if apiKey != "" {
		base.APIKey = apiKey
	}
	return base
}

func ClampReasoning(effort *ai.ThinkingLevel) *ai.ThinkingLevel {
	if effort == nil {
		return nil
	}
	if *effort == ai.ThinkingLevelXHigh {
		high := ai.ThinkingLevelHigh
		return &high
	}
	return effort
}

type AdjustedThinkingTokens struct {
	MaxTokens      int
	ThinkingBudget int
}

func AdjustMaxTokensForThinking(baseMaxTokens *int, modelMaxTokens int, reasoningLevel ai.ThinkingLevel, customBudgets *ai.ThinkingBudgets) AdjustedThinkingTokens {
	budgets := map[ai.ThinkingLevel]int{
		ai.ThinkingLevelMinimal: 1024,
		ai.ThinkingLevelLow:     2048,
		ai.ThinkingLevelMedium:  8192,
		ai.ThinkingLevelHigh:    16384,
	}
	if customBudgets != nil {
		if customBudgets.Minimal != nil {
			budgets[ai.ThinkingLevelMinimal] = *customBudgets.Minimal
		}
		if customBudgets.Low != nil {
			budgets[ai.ThinkingLevelLow] = *customBudgets.Low
		}
		if customBudgets.Medium != nil {
			budgets[ai.ThinkingLevelMedium] = *customBudgets.Medium
		}
		if customBudgets.High != nil {
			budgets[ai.ThinkingLevelHigh] = *customBudgets.High
		}
	}

	level := reasoningLevel
	if level == ai.ThinkingLevelXHigh {
		level = ai.ThinkingLevelHigh
	}
	thinkingBudget := budgets[level]
	maxTokens := modelMaxTokens
	if baseMaxTokens != nil {
		maxTokens = minInt(*baseMaxTokens+thinkingBudget, modelMaxTokens)
	}
	if maxTokens <= thinkingBudget {
		thinkingBudget = max(0, maxTokens-1024)
	}
	return AdjustedThinkingTokens{MaxTokens: maxTokens, ThinkingBudget: thinkingBudget}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
