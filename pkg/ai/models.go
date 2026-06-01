package ai

var extendedThinkingLevels = []ModelThinkingLevel{
	ModelThinkingLevelOff,
	ModelThinkingLevelMinimal,
	ModelThinkingLevelLow,
	ModelThinkingLevelMedium,
	ModelThinkingLevelHigh,
	ModelThinkingLevelXHigh,
}

const (
	ModelThinkingLevelMinimal ModelThinkingLevel = "minimal"
	ModelThinkingLevelLow     ModelThinkingLevel = "low"
	ModelThinkingLevelMedium  ModelThinkingLevel = "medium"
	ModelThinkingLevelHigh    ModelThinkingLevel = "high"
	ModelThinkingLevelXHigh   ModelThinkingLevel = "xhigh"
)

func GetSupportedThinkingLevels(model Model) []ModelThinkingLevel {
	if !model.Reasoning {
		return []ModelThinkingLevel{ModelThinkingLevelOff}
	}
	levels := make([]ModelThinkingLevel, 0, len(extendedThinkingLevels))
	for _, level := range extendedThinkingLevels {
		if level == ModelThinkingLevelOff {
			if mapped, ok := model.ThinkingLevelMap[level]; ok && mapped == nil {
				continue
			}
			levels = append(levels, level)
			continue
		}
		mapped, ok := model.ThinkingLevelMap[level]
		if ok && mapped == nil {
			continue
		}
		if level == ModelThinkingLevelXHigh && !ok {
			continue
		}
		levels = append(levels, level)
	}
	return levels
}

func ClampThinkingLevel(model Model, level ModelThinkingLevel) ModelThinkingLevel {
	available := GetSupportedThinkingLevels(model)
	if containsThinkingLevel(available, level) {
		return level
	}
	requestedIndex := thinkingLevelIndex(level)
	if requestedIndex < 0 {
		return firstThinkingLevelOrOff(available)
	}
	for i := requestedIndex; i < len(extendedThinkingLevels); i++ {
		candidate := extendedThinkingLevels[i]
		if containsThinkingLevel(available, candidate) {
			return candidate
		}
	}
	for i := requestedIndex - 1; i >= 0; i-- {
		candidate := extendedThinkingLevels[i]
		if containsThinkingLevel(available, candidate) {
			return candidate
		}
	}
	return firstThinkingLevelOrOff(available)
}

func ModelsAreEqual(a *Model, b *Model) bool {
	if a == nil || b == nil {
		return false
	}
	return a.ID == b.ID && a.Provider == b.Provider
}

func thinkingLevelIndex(level ModelThinkingLevel) int {
	for index, candidate := range extendedThinkingLevels {
		if candidate == level {
			return index
		}
	}
	return -1
}

func containsThinkingLevel(levels []ModelThinkingLevel, level ModelThinkingLevel) bool {
	for _, candidate := range levels {
		if candidate == level {
			return true
		}
	}
	return false
}

func firstThinkingLevelOrOff(levels []ModelThinkingLevel) ModelThinkingLevel {
	if len(levels) == 0 {
		return ModelThinkingLevelOff
	}
	return levels[0]
}
