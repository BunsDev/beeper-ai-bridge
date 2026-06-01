package connector

import "strings"

const (
	toolModeOff    = "off"
	toolModeBeeper = "beeper"
	toolModeNative = "native"

	defaultSearchMode = toolModeBeeper
	defaultFetchMode  = toolModeBeeper
)

func roomSearchMode(config RoomConfig) string {
	if config.SearchMode != "" {
		return normalizedToolMode(config.SearchMode, defaultSearchMode)
	}
	return searchModeFromDisabled(config.DisabledTools)
}

func roomFetchMode(config RoomConfig) string {
	if config.FetchMode != "" {
		return normalizedToolMode(config.FetchMode, defaultFetchMode)
	}
	return fetchModeFromDisabled(config.DisabledTools)
}

func normalizedToolMode(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case toolModeOff, toolModeBeeper, toolModeNative:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return fallback
	}
}

func searchModeFromDisabled(disabled []string) string {
	if toolDisabled(disabled, "web_search") || toolDisabled(disabled, "search") {
		return toolModeOff
	}
	return defaultSearchMode
}

func fetchModeFromDisabled(disabled []string) string {
	if toolDisabled(disabled, "fetch") {
		return toolModeOff
	}
	return defaultFetchMode
}

func toolModeStateContent(config RoomConfig) map[string]any {
	content := map[string]any{
		"search": roomSearchMode(config),
		"fetch":  roomFetchMode(config),
	}
	if len(config.DisabledTools) > 0 {
		content["disabled"] = config.DisabledTools
	}
	return content
}
