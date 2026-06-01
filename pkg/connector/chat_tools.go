package connector

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/chattools"
)

type chatToolsApprovalContext struct {
	publisher bridgev2.BeeperStreamPublisher
	active    *activeAIRun
}

func (cl *Client) chatTools(msg *bridgev2.MatrixMessage, meta *aiid.PortalMetadata, roomConfig RoomConfig, provider aiid.ProviderConfig, model ai.Model, chatFirstMessageAt string, approvalContext ...chatToolsApprovalContext) []agent.AgentTool[any] {
	if !modelSupportsAgentTools(model) {
		return nil
	}
	chatID := ""
	chatTitle := ""
	if meta != nil {
		chatID = meta.SessionID
	}
	if msg != nil && msg.Portal != nil && msg.Portal.NameSet {
		chatTitle = msg.Portal.Name
	}
	info := chattools.SessionInfo{
		ChatID:             chatID,
		ChatTitle:          chatTitle,
		ChatFirstMessageAt: chatFirstMessageAt,
		SelectedModel:      model.ID,
		SelectedReasoning:  cl.reasoningLevelForModel(model, roomConfig),
		DisabledTools:      roomConfig.DisabledTools,
		LastKnownTimestamp: formatSessionTimestampUTC(matrixEventTime(nil)),
	}
	if msg != nil {
		info.LastKnownTimestamp = formatSessionTimestampUTC(matrixEventTime(msg.Event))
	}
	search := cl.searchOptions(roomConfig, provider)
	fetch := chattools.FetchOptions{
		Timeout:  time.Duration(cl.Main.Config.Fetch.TimeoutMS) * time.Millisecond,
		MaxBytes: cl.Main.Config.Fetch.MaxBytes,
		MaxChars: cl.Main.Config.Fetch.MaxChars,
	}
	if provider.ID == aiid.DefaultProvider && provider.BaseURL != "" {
		if token, err := cl.defaultProviderBearerToken(); err == nil {
			if endpoint, err := aiServicesToolURL(provider.BaseURL, "fetch"); err == nil {
				fetch.ToolEndpoint = endpoint
				fetch.APIKey = token
			}
		}
	}
	sessionOptions := chattools.SessionOptions{}
	var approvals chatToolsApprovalContext
	if len(approvalContext) > 0 {
		approvals = approvalContext[0]
	}
	if msg != nil && msg.Portal != nil && approvals.publisher != nil && approvals.active != nil {
		portal := msg.Portal
		sessionOptions.ResolveProfile = func(ctx context.Context, toolCallID string) (*chattools.SessionProfile, error) {
			return cl.resolveBeeperProfileForSession(ctx, portal, approvals.publisher, approvals.active, toolCallID)
		}
	}
	return chattools.ToolsWithOptions(info, fetch, search, sessionOptions)
}

func formatSessionTimestampUTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func modelSupportsAgentTools(model ai.Model) bool {
	if model.Provider == ai.ProviderGoogleVertex && modelHasOutputModality(model, "image") {
		return false
	}
	if model.Compat == nil {
		return true
	}
	supported, ok := model.Compat["tools_supported"].(bool)
	return !ok || supported
}

func modelHasOutputModality(model ai.Model, modality string) bool {
	for _, output := range model.Output {
		if output == modality {
			return true
		}
	}
	return false
}

func (cl *Client) searchOptions(roomConfig RoomConfig, provider aiid.ProviderConfig) chattools.SearchOptions {
	if toolDisabled(roomConfig.DisabledTools, "web_search") || provider.ID != aiid.DefaultProvider || provider.BaseURL == "" {
		return chattools.SearchOptions{}
	}
	token, err := cl.defaultProviderBearerToken()
	if err != nil {
		return chattools.SearchOptions{}
	}
	endpoint, err := aiServicesToolURL(provider.BaseURL, "web_search")
	if err != nil {
		return chattools.SearchOptions{}
	}
	return chattools.SearchOptions{
		Enabled:  true,
		Endpoint: endpoint,
		APIKey:   token,
		Timeout:  10 * time.Second,
	}
}

func aiServicesToolURL(proxyBaseURL string, tool string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(normalizeResponsesBaseURL(proxyBaseURL), "/"))
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(trimAIProxyProviderPath(parsed.Path), "/") + "/tools/" + tool
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (cl *Client) systemPrompt(roomConfig RoomConfig) string {
	base := strings.TrimSpace(cl.Main.Config.DefaultSystemPrompt)
	room := strings.TrimSpace(roomConfig.AdditionalPrompt)
	switch {
	case base == "":
		return room
	case room == "":
		return base
	default:
		return base + "\n\n" + room
	}
}

func (cl *Client) configuredReasoningLevel(model ai.Model, roomConfig RoomConfig) string {
	if roomConfig.ThinkingLevel != "" {
		return roomConfig.ThinkingLevel
	}
	if model.DefaultThinkingLevel != "" {
		return string(model.DefaultThinkingLevel)
	}
	return cl.Main.Config.DefaultReasoningLevel
}

func (cl *Client) reasoningLevelForModel(model ai.Model, roomConfig RoomConfig) string {
	level := ai.ModelThinkingLevel(cl.configuredReasoningLevel(model, roomConfig))
	if roomConfig.ThinkingLevel == "" {
		level = clampRoomReasoningLevel(model, level)
	}
	return string(level)
}

func (cl *Client) validateReasoningLevel(model ai.Model, roomConfig RoomConfig) error {
	rawLevel := cl.configuredReasoningLevel(model, roomConfig)
	if !validRoomReasoningLevel(rawLevel) {
		return fmt.Errorf("reasoning level %q is invalid", rawLevel)
	}
	level := ai.ModelThinkingLevel(rawLevel)
	if roomConfig.ThinkingLevel == "" {
		level = clampRoomReasoningLevel(model, level)
	}
	for _, supported := range ai.GetSupportedThinkingLevels(model) {
		if supported == level {
			return nil
		}
	}
	return fmt.Errorf("model %s does not support reasoning level %q", model.ID, level)
}

func clampRoomReasoningLevel(model ai.Model, level ai.ModelThinkingLevel) ai.ModelThinkingLevel {
	return ai.ClampThinkingLevel(model, level)
}

func roomThinkingLevelSupported(model ai.Model, want ai.ModelThinkingLevel) bool {
	for _, supported := range ai.GetSupportedThinkingLevels(model) {
		if supported == want {
			return true
		}
	}
	return false
}

func toolDisabled(disabled []string, name string) bool {
	for _, disabledName := range disabled {
		if disabledName == name {
			return true
		}
	}
	return false
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}
