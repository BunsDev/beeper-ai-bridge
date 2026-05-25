package aiid

import (
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type ProviderConfig struct {
	ID             string                   `json:"id" yaml:"id"`
	DisplayName    string                   `json:"display_name" yaml:"display_name"`
	API            ai.Api                   `json:"api" yaml:"api"`
	Provider       ai.Provider              `json:"provider" yaml:"provider"`
	BaseURL        string                   `json:"base_url" yaml:"base_url"`
	APIKey         string                   `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	Headers        map[string]string        `json:"headers,omitempty" yaml:"headers,omitempty"`
	DefaultModel   string                   `json:"default_model,omitempty" yaml:"default_model,omitempty"`
	AllowedModels  []string                 `json:"allowed_models,omitempty" yaml:"allowed_models,omitempty"`
	ModelOverrides map[string]ModelOverride `json:"model_overrides,omitempty" yaml:"model_overrides,omitempty"`
	Models         []ai.Model               `json:"models,omitempty" yaml:"models,omitempty"`
	Enabled        bool                     `json:"enabled" yaml:"enabled"`
}

type ModelOverride struct {
	Name          string            `json:"name,omitempty" yaml:"name,omitempty"`
	API           ai.Api            `json:"api,omitempty" yaml:"api,omitempty"`
	BaseURL       string            `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Reasoning     *bool             `json:"reasoning,omitempty" yaml:"reasoning,omitempty"`
	Input         []string          `json:"input,omitempty" yaml:"input,omitempty"`
	ContextWindow int               `json:"context_window,omitempty" yaml:"context_window,omitempty"`
	MaxTokens     int               `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Headers       map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Compat        map[string]any    `json:"compat,omitempty" yaml:"compat,omitempty"`
}

type UserLoginMetadata struct {
	SyntheticDefault  bool                      `json:"synthetic_default,omitempty"`
	Providers         map[string]ProviderConfig `json:"providers,omitempty"`
	DefaultProviderID string                    `json:"default_provider_id,omitempty"`
	DefaultModelID    string                    `json:"default_model_id,omitempty"`
}

type PortalMetadata struct {
	SessionID          string   `json:"session_id,omitempty"`
	SelectedProviderID string   `json:"selected_provider_id,omitempty"`
	SelectedModelID    string   `json:"selected_model_id,omitempty"`
	AdditionalPrompt   string   `json:"additional_prompt,omitempty"`
	ThinkingLevel      string   `json:"thinking_level,omitempty"`
	DisabledTools      []string `json:"disabled_tools,omitempty"`
	SessionTitle       string   `json:"session_title,omitempty"`
	LastRunID          string   `json:"last_run_id,omitempty"`
	RoomStateEventID   string   `json:"room_state_event_id,omitempty"`
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
