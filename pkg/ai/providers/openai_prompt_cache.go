package providers

const OpenAIPromptCacheKeyMaxLength = 64

func ClampOpenAIPromptCacheKey(key string) string {
	chars := []rune(key)
	if len(chars) <= OpenAIPromptCacheKeyMaxLength {
		return key
	}
	return string(chars[:OpenAIPromptCacheKeyMaxLength])
}

func ClampOpenAIPromptCacheKeyPtr(key *string) *string {
	if key == nil {
		return nil
	}
	clamped := ClampOpenAIPromptCacheKey(*key)
	return &clamped
}
