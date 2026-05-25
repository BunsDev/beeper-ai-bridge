package connector

import (
	"context"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

func TestParseAISlashCommand(t *testing.T) {
	tests := []struct {
		body string
		name string
		arg  string
		ok   bool
	}{
		{body: "/model gpt-5", name: "model", arg: "gpt-5", ok: true},
		{body: " /reasoning high ", name: "reasoning", arg: "high", ok: true},
		{body: "/reasoniing low", name: "reasoniing", arg: "low", ok: true},
		{body: "/system-prompt be terse", name: "system-prompt", arg: "be terse", ok: true},
		{body: "/unknown nope", ok: false},
		{body: "hello /model gpt-5", ok: false},
	}
	for _, tt := range tests {
		got, ok := parseAISlashCommand(tt.body)
		if ok != tt.ok {
			t.Fatalf("%q ok=%v, want %v", tt.body, ok, tt.ok)
		}
		if !ok {
			continue
		}
		if got.name != tt.name || got.arg != tt.arg {
			t.Fatalf("%q parsed as %#v, want name=%q arg=%q", tt.body, got, tt.name, tt.arg)
		}
	}
}

func TestResolveCanonicalRoomModelUsesDefaultProviderForBareModel(t *testing.T) {
	client := canonicalTestClient()
	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ModelID: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "gpt-5" || canonical != "beeper/gpt-5" {
		t.Fatalf("unexpected canonical model %q %#v", canonical, model)
	}
}

func TestResolveCanonicalRoomModelPreservesFullProviderModel(t *testing.T) {
	client := canonicalTestClient()
	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ProviderID: "openrouter", ModelID: "openai/gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "openai/gpt-5" || canonical != "openrouter/openai/gpt-5" {
		t.Fatalf("unexpected canonical model %q %#v", canonical, model)
	}
}

func TestRoomReasoningValidationSyntax(t *testing.T) {
	for _, level := range []string{"", "off", "low", "medium", "high"} {
		if !validRoomReasoningLevel(level) {
			t.Fatalf("expected %q to be valid", level)
		}
	}
	for _, level := range []string{"minimal", "xhigh", "banana"} {
		if validRoomReasoningLevel(level) {
			t.Fatalf("expected %q to be invalid", level)
		}
	}
}

func canonicalTestClient() *Client {
	conn := &Connector{}
	conn.Config.ApplyDefaults()
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID: "login",
		Metadata: &aiid.UserLoginMetadata{
			DefaultProviderID: "beeper",
			Providers: map[string]aiid.ProviderConfig{
				"beeper": {
					ID:            "beeper",
					Provider:      ai.ProviderOpenAI,
					API:           ai.ApiOpenAIResponses,
					DefaultModel:  "gpt-5",
					AllowedModels: []string{"gpt-5"},
					Enabled:       true,
				},
				"openrouter": {
					ID:            "openrouter",
					Provider:      ai.ProviderOpenRouter,
					API:           ai.ApiOpenAICompletions,
					DefaultModel:  "openai/gpt-5",
					AllowedModels: []string{"openai/gpt-5"},
					Enabled:       true,
				},
			},
		},
	}}
	return &Client{Main: conn, UserLogin: login}
}
