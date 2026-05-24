package aiid

import (
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type ProviderConfig struct {
	ID           string            `json:"id"`
	DisplayName  string            `json:"display_name"`
	API          ai.Api            `json:"api"`
	Provider     ai.Provider       `json:"provider"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"api_key,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	DefaultModel string            `json:"default_model,omitempty"`
	Models       []ai.Model        `json:"models,omitempty"`
	Enabled      bool              `json:"enabled"`
}

type UserLoginMetadata struct {
	SyntheticDefault  bool                      `json:"synthetic_default,omitempty"`
	Providers         map[string]ProviderConfig `json:"providers,omitempty"`
	DefaultProviderID string                    `json:"default_provider_id,omitempty"`
	DefaultModelID    string                    `json:"default_model_id,omitempty"`
}

type PortalMetadata struct {
	SessionID          string `json:"session_id,omitempty"`
	SelectedLoginID    string `json:"selected_login_id,omitempty"`
	SelectedProviderID string `json:"selected_provider_id,omitempty"`
	SelectedModelID    string `json:"selected_model_id,omitempty"`
	SystemPrompt       string `json:"system_prompt,omitempty"`
	ThinkingLevel      string `json:"thinking_level,omitempty"`
	ToolsEnabled       bool   `json:"tools_enabled,omitempty"`
	Cwd                string `json:"cwd,omitempty"`
	LastRunID          string `json:"last_run_id,omitempty"`
	RoomStateEventID   string `json:"room_state_event_id,omitempty"`
}

type GhostMetadata struct {
	ProviderID string `json:"provider_id,omitempty"`
	ModelID    string `json:"model_id,omitempty"`
}

type MessageMetadata struct {
	SessionEntryID string   `json:"session_entry_id,omitempty"`
	Role           string   `json:"role,omitempty"`
	RunID          string   `json:"run_id,omitempty"`
	ProviderID     string   `json:"provider_id,omitempty"`
	ModelID        string   `json:"model_id,omitempty"`
	ResponseID     string   `json:"response_id,omitempty"`
	ContentIndex   int      `json:"content_index,omitempty"`
	Usage          ai.Usage `json:"usage,omitempty"`
	StopReason     string   `json:"stop_reason,omitempty"`
	ErrorMessage   string   `json:"error_message,omitempty"`
	StreamStatus   string   `json:"stream_status,omitempty"`
}

type ReactionMetadata struct{}

type MediaMetadata struct {
	LoginID      string `json:"login_id,omitempty"`
	ProviderID   string `json:"provider_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	EntryID      string `json:"entry_id,omitempty"`
	ContentIndex int    `json:"content_index,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	Retrieval    any    `json:"retrieval,omitempty"`
}
