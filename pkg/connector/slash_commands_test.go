package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

func TestParseAISlashCommand(t *testing.T) {
	tests := []struct {
		body string
		name string
		arg  string
		ok   bool
	}{
		{body: "/model gpt-5", name: "model", arg: "gpt-5", ok: true},
		{body: "/model", name: "model", ok: true},
		{body: " /reasoning high ", name: "reasoning", arg: "high", ok: true},
		{body: "/reasoning", name: "reasoning", ok: true},
		{body: "/reasoniing low", ok: false},
		{body: "/system-prompt be terse", name: "system-prompt", arg: "be terse", ok: true},
		{body: "/system-prompt", name: "system-prompt", ok: true},
		{body: "/help model", name: "help", arg: "model", ok: true},
		{body: "/compact focus on decisions", name: "compact", arg: "focus on decisions", ok: true},
		{body: "/abort", name: "abort", ok: true},
		{body: "/stop", name: "abort", ok: true},
		{body: "/session", name: "session", ok: true},
		{body: "/limits", name: "limits", ok: true},
		{body: "/approve approval-1 always", name: "approve", arg: "approval-1 always", ok: true},
		{body: "/reset-approvals", name: "reset-approvals", ok: true},
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

func TestParseAICommandMessage(t *testing.T) {
	tests := []struct {
		name    string
		content *event.MessageEventContent
		want    aiSlashCommand
		ok      bool
	}{
		{
			name:    "visible slash command",
			content: &event.MessageEventContent{MsgType: event.MsgText, Body: "/model gpt-5"},
			want:    aiSlashCommand{name: "model", arg: "gpt-5"},
			ok:      true,
		},
		{
			name:    "hidden slash command",
			content: &event.MessageEventContent{MsgType: matrixCommandMsgType, Body: "/abort"},
			want:    aiSlashCommand{name: "abort"},
			ok:      true,
		},
		{
			name:    "hidden bridge-prefixed command",
			content: &event.MessageEventContent{MsgType: matrixCommandMsgType, Body: "!ai stop"},
			want:    aiSlashCommand{name: "abort"},
			ok:      true,
		},
		{
			name:    "hidden bridge-prefixed command with args",
			content: &event.MessageEventContent{MsgType: matrixCommandMsgType, Body: "!ai model gpt-5"},
			want:    aiSlashCommand{name: "model", arg: "gpt-5"},
			ok:      true,
		},
		{
			name:    "hidden bare command ignored",
			content: &event.MessageEventContent{MsgType: matrixCommandMsgType, Body: "abort"},
			ok:      false,
		},
		{
			name:    "visible bridge-prefixed command ignored",
			content: &event.MessageEventContent{MsgType: event.MsgText, Body: "!ai stop"},
			ok:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAICommandMessage(tt.content)
			if ok != tt.ok {
				t.Fatalf("ok=%v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("parsed %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCanonicalAICommandNameAliases(t *testing.T) {
	tests := map[string]string{
		"abort":   "abort",
		" stop ":  "abort",
		"ai-help": "help",
		"MODEL":   "model",
	}
	for input, want := range tests {
		if got := canonicalAICommandName(input); got != want {
			t.Fatalf("canonicalAICommandName(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestApprovalResponseFromCommandAliases(t *testing.T) {
	response, ok := approvalResponseFromCommand("approval-1", "always")
	if !ok || !response.Approved || !response.Always || response.Choice != aistream.ApprovalChoiceAlwaysApprove {
		t.Fatalf("always approval response = %#v ok=%v", response, ok)
	}
	response, ok = approvalResponseFromCommand("approval-1", "deny")
	if !ok || response.Approved || response.Reason != "denied" {
		t.Fatalf("deny approval response = %#v ok=%v", response, ok)
	}
}

func TestAISlashCommandHelpCatalogUsesDefinitions(t *testing.T) {
	help := aiSlashCommandHelp("")
	seen := map[string]bool{}
	for _, def := range aiSlashCommandDefinitions() {
		if def.name == "" {
			t.Fatal("registered command has empty name")
		}
		if seen[def.name] {
			t.Fatalf("registered command %q more than once", def.name)
		}
		seen[def.name] = true
		if def.run == nil {
			t.Fatalf("registered command %q has no handler", def.name)
		}
		if !strings.Contains(help, "`"+def.usage+"`") {
			t.Fatalf("help catalog is missing usage %q:\n%s", def.usage, help)
		}
		if !strings.Contains(help, def.description) {
			t.Fatalf("help catalog is missing description %q:\n%s", def.description, help)
		}
		if _, ok := parseAISlashCommand(def.usage); !ok {
			t.Fatalf("registered command usage %q is not parseable", def.usage)
		}
	}
}

func TestAISlashCommandHelpForSpecificCommand(t *testing.T) {
	help := aiSlashCommandHelp("/model")
	if !strings.Contains(help, "Usage: /model [model]") {
		t.Fatalf("specific help is missing model usage:\n%s", help)
	}
	if strings.Contains(help, "/reasoning") {
		t.Fatalf("specific help included the full catalog:\n%s", help)
	}

	help = aiSlashCommandHelp("/limits")
	if strings.Contains(strings.ToLower(help), "raw") {
		t.Fatalf("limits help advertised raw debugging mode:\n%s", help)
	}
}

func TestCurrentCommandResponseText(t *testing.T) {
	if got := displayReasoningLevel(""); got != "off" {
		t.Fatalf("empty reasoning level = %q, want off", got)
	}
	model := ai.Model{ID: "anthropic/claude-opus-4.5", Reasoning: true}
	status := reasoningStatusText("", "beeper/anthropic/claude-opus-4.5", model)
	if !strings.Contains(status, "Current reasoning is `off` for `beeper/anthropic/claude-opus-4.5`.") {
		t.Fatalf("reasoning status is missing current value:\n%s", status)
	}
	if !strings.Contains(status, "Options: `off`, `minimal`, `low`, `medium`, `high`.") {
		t.Fatalf("reasoning status is missing supported options:\n%s", status)
	}
	modelStatus := canonicalTestClient().modelStatusText("beeper/gpt-5.5", "off", aiid.ProviderConfig{
		ID:     "beeper",
		Models: []ai.Model{{ID: "gpt-5.5"}, {ID: "openai/gpt-5.5"}},
	})
	if !strings.Contains(modelStatus, "Current model is `beeper/gpt-5.5`. Current reasoning is `off`.") {
		t.Fatalf("model status is missing current value:\n%s", modelStatus)
	}
	if !strings.Contains(modelStatus, "Options: `beeper/gpt-5.5`, `beeper/openai/gpt-5.5`.") {
		t.Fatalf("model status is missing available options:\n%s", modelStatus)
	}
	if got := currentSystemPromptText(RoomConfig{}); got != "No additional system prompt is set." {
		t.Fatalf("empty system prompt text = %q", got)
	}
	promptStatus := systemPromptStatusText(RoomConfig{})
	if !strings.Contains(promptStatus, "Options: `/system-prompt <prompt>`, `/system-prompt clear`.") {
		t.Fatalf("system prompt status is missing options:\n%s", promptStatus)
	}
	prompt := currentSystemPromptText(RoomConfig{AdditionalPrompt: "be terse"})
	if !strings.Contains(prompt, "Current system prompt:") || !strings.Contains(prompt, "```\nbe terse\n```") {
		t.Fatalf("unexpected current prompt text:\n%s", prompt)
	}
}

func TestCommandResponseContentIsVisibleText(t *testing.T) {
	content := commandResponseContent(aiSlashCommandHelp(""))
	if content.MsgType != event.MsgText {
		t.Fatalf("command response msgtype=%s, want %s", content.MsgType, event.MsgText)
	}
	if content.Format != event.FormatHTML {
		t.Fatalf("command response format=%s, want %s", content.Format, event.FormatHTML)
	}
	if !strings.Contains(content.Body, "AI Bridge commands:") {
		t.Fatalf("command response body did not include help catalog:\n%s", content.Body)
	}
	if !strings.Contains(content.FormattedBody, "<code>/help [command]</code>") {
		t.Fatalf("command response formatted body did not render command usage as HTML:\n%s", content.FormattedBody)
	}
}

func TestCommandResponseContentRendersMarkdownTables(t *testing.T) {
	content := commandResponseContent("| One | Two |\n| --- | ---: |\n| A | `1` |\n")
	if content.Format != event.FormatHTML || !strings.Contains(content.FormattedBody, "<table>") {
		t.Fatalf("command response did not render markdown table as Matrix HTML: %#v", content)
	}
	if !strings.Contains(content.FormattedBody, "<code>1</code>") {
		t.Fatalf("command response table did not preserve markdown cell formatting:\n%s", content.FormattedBody)
	}
}

func TestCommandFinalAICarriesMarkdownAsFinalAssistantMessage(t *testing.T) {
	text := "AI limits\n\n## Models\n\n| Window | Left |\n| --- | ---: |\n| Daily | `75%` |\n"
	payload := commandFinalAI(text, "message-1", "thread-1", "beeper/gpt-5", "ai", "AI", time.Unix(10, 0))
	if payload.Schema != aistream.BeeperAISchema || payload.Protocol != "ag-ui" || payload.Kind != aistream.AIKindFinal {
		t.Fatalf("unexpected command AI envelope: %#v", payload)
	}
	if payload.Message == nil || payload.Message.Role != agui.RoleAssistant || payload.Message.ID != "message-1" {
		t.Fatalf("unexpected command AI message: %#v", payload.Message)
	}
	if len(payload.Message.Parts) != 1 || payload.Message.Parts[0]["type"] != "text" || payload.Message.Parts[0]["content"] != text {
		t.Fatalf("command AI message did not preserve markdown text part: %#v", payload.Message.Parts)
	}
	if len(payload.Events) != 1 || payload.Events[0].Event.Type() != agui.EventRunFinished {
		t.Fatalf("command AI final payload missing terminal event: %#v", payload.Events)
	}
}

func TestProviderCommandTextDoesNotExposeSecrets(t *testing.T) {
	provider := aiid.ProviderConfig{
		ID:           "custom",
		DisplayName:  "Custom",
		API:          ai.ApiOpenAIResponses,
		Provider:     "custom",
		BaseURL:      "https://example.test/v1",
		APIKey:       "secret-key",
		RefreshToken: "refresh-secret",
		Headers:      map[string]string{"Authorization": "Bearer header-secret"},
		DefaultModel: "model-a",
		Models:       []ai.Model{{ID: "model-a"}},
	}
	text := providerText(providerResponse(provider))
	for _, secret := range []string{"secret-key", "refresh-secret", "header-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("provider command text leaked %q:\n%s", secret, text)
		}
	}
	if !strings.Contains(text, "Provider `custom`") || !strings.Contains(text, "Default model: `model-a`") {
		t.Fatalf("provider command text lost public fields:\n%s", text)
	}
}

func TestCommandRejectedErrorSendsFailedStatusNoticeWithExactMessage(t *testing.T) {
	text := `AI room settings rejected: reasoning level "invalidvalue" is invalid`
	err := commandRejectedError(text)
	var status bridgev2.MessageStatus
	if !errors.As(err, &status) {
		t.Fatalf("commandRejectedError did not return message status: %T", err)
	}
	if status.Status != event.MessageStatusFail {
		t.Fatalf("status=%s, want %s", status.Status, event.MessageStatusFail)
	}
	if status.ErrorReason != event.MessageStatusUnsupported {
		t.Fatalf("reason=%s, want %s", status.ErrorReason, event.MessageStatusUnsupported)
	}
	if !status.SendNotice || !status.IsCertain || !status.ErrorAsMessage {
		t.Fatalf("status flags not set for visible exact notice: %#v", status)
	}
	info := &bridgev2.MessageStatusEventInfo{
		RoomID:        "!room:example.com",
		SourceEventID: "$event",
		EventType:     event.EventMessage,
	}
	mss := status.ToMSSEvent(info)
	if mss.Message != text || mss.InternalError != text {
		t.Fatalf("message status did not expose exact error: message=%q internal=%q", mss.Message, mss.InternalError)
	}
	notice := status.ToNoticeEvent(info)
	if notice.MsgType != event.MsgNotice || !strings.Contains(notice.Body, text) {
		t.Fatalf("notice did not expose exact error: %#v", notice)
	}
}

func TestSessionCommandStatsFromEntries(t *testing.T) {
	stats, err := sessionCommandStatsFromEntries([]json.RawMessage{
		rawSessionEntry(t, map[string]any{"type": "message", "message": map[string]any{"role": "user"}}),
		rawSessionEntry(t, map[string]any{"type": "message", "message": map[string]any{"role": "assistant"}}),
		rawSessionEntry(t, map[string]any{"type": "message", "message": map[string]any{"role": "toolResult"}}),
		rawSessionEntry(t, map[string]any{"type": "compaction"}),
		rawSessionEntry(t, map[string]any{"type": "model_change"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalEntries != 5 || stats.Messages != 3 || stats.UserMessages != 1 || stats.AssistantMessages != 1 || stats.ToolResultMessages != 1 || stats.Compactions != 1 {
		t.Fatalf("unexpected session stats: %#v", stats)
	}
}

func TestFormatSessionCommandInfo(t *testing.T) {
	text := formatSessionCommandInfo(sessionCommandInfo{
		SessionID:     "session-1",
		CreatedAt:     "2026-05-30T00:00:00Z",
		RoomProvider:  "beeper",
		RoomModel:     "beeper/gpt-5.5",
		RoomReasoning: "off",
		SystemPrompt:  true,
		Responding:    true,
		Stats: sessionCommandStats{
			TotalEntries:       4,
			Messages:           3,
			UserMessages:       1,
			AssistantMessages:  1,
			ToolResultMessages: 1,
			Compactions:        1,
		},
	})
	for _, want := range []string{
		"Status: `responding`",
		"ID: `session-1`",
		"Room model: `beeper/gpt-5.5`",
		"System prompt: `yes, 0 chars`",
		"Messages: `3` total, `1` user, `1` assistant, `1` tool results",
		"Compactions: `1`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("session info missing %q:\n%s", want, text)
		}
	}

	text = formatSessionCommandInfo(sessionCommandInfo{RoomProvider: "beeper", RoomModel: "beeper/gpt-5.5", RoomReasoning: "off"})
	if !strings.Contains(text, "No AI session has been started in this room yet.") {
		t.Fatalf("empty session info missing no-session text:\n%s", text)
	}
}

func TestAIServicesLimitsURLStripsProviderProxyPaths(t *testing.T) {
	tests := map[string]string{
		"https://ai-services.beeper.com/proxy/openai/v1":          "https://ai-services.beeper.com/limits",
		"https://ai-services.beeper.com/proxy/openrouter/v1":      "https://ai-services.beeper.com/limits",
		"https://ai-services.beeper.com/proxy/anthropic":          "https://ai-services.beeper.com/limits",
		"https://ai-services.beeper.com/proxy/vertex":             "https://ai-services.beeper.com/limits",
		"https://ai-services.beeper.com/proxy/a8c/v1":             "https://ai-services.beeper.com/limits",
		"https://ai-services.beeper.com/proxy/_/v1/responses":     "https://ai-services.beeper.com/limits",
		"https://ai-services.beeper.com/dev/proxy/openai/v1":      "https://ai-services.beeper.com/dev/limits",
		"https://ai-services.beeper.com/dev/proxy/openrouter/v1/": "https://ai-services.beeper.com/dev/limits",
	}
	for input, want := range tests {
		got, err := aiServicesLimitsURL(input)
		if err != nil {
			t.Fatalf("aiServicesLimitsURL(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("aiServicesLimitsURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFetchAIServicesLimitsUsesAppserviceBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/limits" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		payload, ok := strings.CutPrefix(auth, "Bearer "+aiServicesAppserviceTokenPrefix)
		if !ok {
			t.Fatalf("expected appservice bearer token, got %q", auth)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			t.Fatal(err)
		}
		var token aiServicesAppserviceToken
		if err = json.Unmarshal(decoded, &token); err != nil {
			t.Fatal(err)
		}
		if token.ASToken != "as-token" || token.Username != "alice" {
			t.Fatalf("unexpected appservice token %#v", token)
		}
		_, _ = w.Write([]byte(`{"windows":{"llm":{"day":{"percentage_left":90,"limit":1000,"used":100,"remaining":900,"reset_at":1893456000000},"week":{"percentage_left":100,"limit":7000,"used":0,"remaining":7000,"reset_at":1893974400000},"month":{"percentage_left":100,"limit":30000,"used":0,"remaining":30000,"reset_at":1896134400000}}}}`))
	}))
	defer server.Close()

	provider := aiid.ProviderConfig{ID: aiid.DefaultProvider, BaseURL: server.URL + "/proxy/openai/v1"}
	client := &Client{
		Main: &Connector{AppServiceToken: "as-token"},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			UserMXID: "@alice:beeper.test",
			Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
				provider.ID: provider,
			}},
		}},
	}
	limits, err := client.fetchAIServicesLimits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if limits.Windows.LLM.Day.Limit != 1000 || limits.Windows.LLM.Day.Used != 100 {
		t.Fatalf("unexpected limits %#v", limits.Windows.LLM.Day)
	}
}

func TestFormatLimitsCommandInfo(t *testing.T) {
	now := time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)
	reset := now.Add(26*time.Hour + 3*time.Minute).UnixMilli()
	text := formatLimitsCommandInfo(aiServicesLimitsResponse{Windows: aiServicesLimitCategories{
		LLM: aiServicesLimitWindows{
			Day:   aiServicesLimitWindow{PercentageLeft: 75, Limit: 1000, Used: 250, Remaining: 750, ResetAtMS: reset},
			Week:  aiServicesLimitWindow{PercentageLeft: 100, Limit: -1, Used: 1234, Remaining: -1, ResetAtMS: reset},
			Month: aiServicesLimitWindow{PercentageLeft: 0, Limit: 30000, Used: 30500, Remaining: -500, ResetAtMS: reset},
		},
		WebTools: aiServicesLimitWindows{
			Day:   aiServicesLimitWindow{PercentageLeft: 99, Limit: 200000, Used: 1, Remaining: 199999, ResetAtMS: reset},
			Week:  aiServicesLimitWindow{PercentageLeft: 100, Limit: 1000000, Used: 0, Remaining: 1000000, ResetAtMS: reset},
			Month: aiServicesLimitWindow{PercentageLeft: 100, Limit: 4000000, Used: 0, Remaining: 4000000, ResetAtMS: reset},
		},
	}}, now)
	for _, want := range []string{
		"AI limits",
		"## Models",
		"| Window | Left | Used | Reset |",
		"| Daily | `75%` | `250 / 1,000` | in 1 day 2 hours 3 minutes |",
		"| Weekly | Unlimited | `1,234` used | in 1 day 2 hours 3 minutes |",
		"| Monthly | **Out** | `30,500 / 30,000` | in 1 day 2 hours 3 minutes |",
		"## Web Search",
		"| Daily | `99%` | `1 / 200,000` | in 1 day 2 hours 3 minutes |",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("limits info missing %q:\n%s", want, text)
		}
	}
	for _, notWant := range []string{"`199,999`", "2030-01-01T00:00:00Z"} {
		if strings.Contains(text, notWant) {
			t.Fatalf("limits info exposed non-summary value %q:\n%s", notWant, text)
		}
	}
}

func TestFormatLimitsCommandInfoShowsPerWindowResetsWhenDifferent(t *testing.T) {
	now := time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)
	text := formatLimitsCommandInfo(aiServicesLimitsResponse{Windows: aiServicesLimitCategories{
		LLM: aiServicesLimitWindows{
			Day:   aiServicesLimitWindow{PercentageLeft: 75, ResetAtMS: now.Add(25*time.Hour + 3*time.Minute).UnixMilli()},
			Week:  aiServicesLimitWindow{PercentageLeft: 100, ResetAtMS: now.Add(7 * 24 * time.Hour).UnixMilli()},
			Month: aiServicesLimitWindow{PercentageLeft: 0, ResetAtMS: now.Add(31 * 24 * time.Hour).UnixMilli()},
		},
	}}, now)
	for _, want := range []string{
		"## Models",
		"| Daily | `75%` | Not reported | in 1 day 1 hour 3 minutes |",
		"| Weekly | `100%` | Not reported | in 7 days |",
		"| Monthly | **Out** | Not reported | in 31 days |",
		"## Web Search",
		"No limits reported.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("limits info missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Everything resets") {
		t.Fatalf("limits info collapsed different reset times:\n%s", text)
	}
}

func TestFormatResetInDoesNotRoundUp(t *testing.T) {
	now := time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)
	tests := map[time.Duration]string{
		45 * time.Second: "less than 1 minute",
		23*time.Hour + 59*time.Minute + 59*time.Second: "23 hours 59 minutes",
		24*time.Hour + 59*time.Minute:                  "1 day 59 minutes",
		26*time.Hour + 3*time.Minute:                   "1 day 2 hours 3 minutes",
	}
	for duration, want := range tests {
		if got := formatResetIn(now.Add(duration), now); got != want {
			t.Fatalf("formatResetIn(%s) = %q, want %q", duration, got, want)
		}
	}
}

func TestFormatRawLimitsCommandInfoShowsExactUsage(t *testing.T) {
	resetAt := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	text := formatRawLimitsCommandInfo(aiServicesLimitsResponse{Windows: aiServicesLimitCategories{
		LLM: aiServicesLimitWindows{
			Day:   aiServicesLimitWindow{PercentageLeft: 75, Limit: 1000, Used: 250, Remaining: 750, ResetAtMS: resetAt.UnixMilli()},
			Week:  aiServicesLimitWindow{PercentageLeft: 100, Limit: -1, Used: 1234, Remaining: -1, ResetAtMS: resetAt.UnixMilli()},
			Month: aiServicesLimitWindow{PercentageLeft: 0, Limit: 30000, Used: 30000, Remaining: 0, ResetAtMS: resetAt.UnixMilli()},
		},
	}}, time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"AI limits raw:",
		"percentage_left=`75`, limit=`1,000`, used=`250`, remaining=`750`, reset_at=`1893456000000` (`2030-01-01T00:00:00Z`, in 1 day)",
		"percentage_left=`100`, limit=`-1`, used=`1,234`, remaining=`-1`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("raw limits info missing %q:\n%s", want, text)
		}
	}
}

func TestResolveCanonicalRoomModelUsesDefaultProviderForBareModel(t *testing.T) {
	client := canonicalTestClient()
	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ModelID: "gpt-5.5"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "gpt-5.5" || canonical != "beeper/gpt-5.5" {
		t.Fatalf("unexpected canonical model %q %#v", canonical, model)
	}
}

func TestResolveCanonicalRoomModelMatchesBareModelByCatalogSuffixOrder(t *testing.T) {
	client := canonicalTestClient()
	meta := client.UserLogin.Metadata.(*aiid.UserLoginMetadata)
	provider := meta.Providers[aiid.DefaultProvider]
	provider.Models = []ai.Model{
		{ID: "anthropic/gpt-5.5", Name: "First GPT 5.5", Provider: ai.ProviderOpenRouter, API: ai.ApiOpenAIResponses},
		{ID: "openai/gpt-5.5", Name: "OpenAI GPT 5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses},
	}
	meta.Providers[provider.ID] = provider

	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ModelID: "gpt-5.5"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "anthropic/gpt-5.5" || canonical != "beeper/anthropic/gpt-5.5" {
		t.Fatalf("unexpected canonical model %q %#v", canonical, model)
	}
}

func TestResolveCanonicalRoomModelPreservesDefaultOpenAICatalogModel(t *testing.T) {
	client := canonicalTestClient()
	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ProviderID: "beeper", ModelID: "openai/gpt-5.5"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "openai/gpt-5.5" || canonical != "beeper/openai/gpt-5.5" {
		t.Fatalf("unexpected canonical model %q %#v", canonical, model)
	}
}

func TestResolveCanonicalRoomModelPreservesFullProviderModel(t *testing.T) {
	client := canonicalTestClient()
	openrouter := aiid.ProviderConfig{
		ID:           "openrouter",
		Provider:     ai.ProviderOpenRouter,
		API:          ai.ApiOpenAICompletions,
		DefaultModel: "openai/gpt-5",
		Models:       []ai.Model{{ID: "openai/gpt-5", Provider: ai.ProviderOpenRouter, API: ai.ApiOpenAICompletions}},
	}
	client.UserLogin.Metadata = &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{openrouter.ID: openrouter}}
	_, model, canonical, err := client.resolveCanonicalRoomModel(context.Background(), RoomConfig{ProviderID: "openrouter", ModelID: "openai/gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "openai/gpt-5" || canonical != "openrouter/openai/gpt-5" {
		t.Fatalf("unexpected canonical model %q %#v", canonical, model)
	}
}

func TestRoomReasoningValidationSyntax(t *testing.T) {
	for _, level := range []string{"", "off", "minimal", "low", "medium", "high", "xhigh"} {
		if !validRoomReasoningLevel(level) {
			t.Fatalf("expected %q to be valid", level)
		}
	}
	for _, level := range []string{"xlow", "banana"} {
		if validRoomReasoningLevel(level) {
			t.Fatalf("expected %q to be invalid", level)
		}
	}
}

func canonicalTestClient() *Client {
	conn := &Connector{}
	conn.Config.ApplyDefaults()
	provider := aiid.ProviderConfig{
		ID:           "beeper",
		Provider:     ai.ProviderOpenAI,
		API:          ai.ApiOpenAIResponses,
		DefaultModel: "gpt-5.5",
		Models:       []ai.Model{{ID: "gpt-5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses}, {ID: "openai/gpt-5.5", Provider: ai.ProviderOpenAI, API: ai.ApiOpenAIResponses}},
	}
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{provider.ID: provider}},
	}}
	return &Client{Main: conn, UserLogin: login}
}

func rawSessionEntry(t *testing.T, entry map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
