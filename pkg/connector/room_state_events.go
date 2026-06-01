package connector

import (
	"reflect"

	"maunium.net/go/mautrix/event"

	"github.com/beeper/ai-bridge/pkg/aiid"
)

type aiRoomModelStateEventContent struct {
	Model         string `json:"model,omitempty"`
	Name          string `json:"name,omitempty"`
	Reasoning     string `json:"reasoning,omitempty"`
	ReasoningMode string `json:"reasoning_mode,omitempty"`
}

type aiRoomPromptStateEventContent struct {
	Prompt string `json:"prompt,omitempty"`
}

type aiRoomToolsStateEventContent struct {
	Disabled []string `json:"disabled,omitempty"`
	Search   string   `json:"search,omitempty"`
	Fetch    string   `json:"fetch,omitempty"`
}

func init() {
	registerAIRoomStateEventContentTypes()
}

func registerAIRoomStateEventContentTypes() {
	event.TypeMap[aiRoomStateEventType(aiid.RoomModelType)] = reflect.TypeOf(aiRoomModelStateEventContent{})
	event.TypeMap[aiRoomStateEventType(aiid.RoomPromptType)] = reflect.TypeOf(aiRoomPromptStateEventContent{})
	event.TypeMap[aiRoomStateEventType(aiid.RoomToolsType)] = reflect.TypeOf(aiRoomToolsStateEventContent{})
}

func aiRoomStateEventType(stateType string) event.Type {
	return event.Type{Type: stateType, Class: event.StateEventType}
}
