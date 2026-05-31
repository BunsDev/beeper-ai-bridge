package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
)

type aiServicesLimitsResponse struct {
	Windows aiServicesLimitCategories `json:"windows"`
}

type aiServicesLimitCategories struct {
	LLM                 aiServicesLimitWindows `json:"llm"`
	WebTools            aiServicesLimitWindows `json:"web_tools"`
	AudioTranscriptions aiServicesLimitWindows `json:"audio_transcriptions"`
	AudioGeneration     aiServicesLimitWindows `json:"audio_generation"`
}

type aiServicesLimitWindows struct {
	Day   aiServicesLimitWindow `json:"day"`
	Week  aiServicesLimitWindow `json:"week"`
	Month aiServicesLimitWindow `json:"month"`
}

type aiServicesLimitWindow struct {
	PercentageLeft int64 `json:"percentage_left"`
	Limit          int64 `json:"limit"`
	Used           int64 `json:"used"`
	Remaining      int64 `json:"remaining"`
	ResetAtMS      int64 `json:"reset_at"`
}

func runLimitsCommand(cl *Client, ctx context.Context, _ *bridgev2.Portal, _ RoomConfig, arg string, responder aiCommandResponder) error {
	raw := false
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
	case "raw":
		raw = true
	default:
		return fmt.Errorf("Usage: /limits")
	}
	limits, err := cl.fetchAIServicesLimits(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	if raw {
		return responder.Reply(ctx, formatRawLimitsCommandInfo(limits, now))
	}
	text := formatLimitsCommandInfo(limits, now)
	if aiResponder, ok := responder.(aiCommandAIResponder); ok {
		return aiResponder.ReplyAI(ctx, text)
	}
	return responder.Reply(ctx, text)
}

func (cl *Client) fetchAIServicesLimits(ctx context.Context) (aiServicesLimitsResponse, error) {
	provider, err := cl.defaultAIProviderForLimits()
	if err != nil {
		return aiServicesLimitsResponse{}, err
	}
	limitsURL, err := aiServicesLimitsURL(provider.BaseURL)
	if err != nil {
		return aiServicesLimitsResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, limitsURL, nil)
	if err != nil {
		return aiServicesLimitsResponse{}, err
	}
	token, err := cl.defaultProviderBearerToken()
	if err != nil {
		return aiServicesLimitsResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := aiutils.WithAIServicesLogging(&http.Client{Timeout: 20 * time.Second})
	resp, err := client.Do(req)
	if err != nil {
		return aiServicesLimitsResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		return aiServicesLimitsResponse{}, fmt.Errorf("AI Services limits returned HTTP %d", resp.StatusCode)
	}
	var body aiServicesLimitsResponse
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return aiServicesLimitsResponse{}, err
	}
	return body, nil
}

func (cl *Client) defaultAIProviderForLimits() (aiid.ProviderConfig, error) {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil {
		return aiid.ProviderConfig{}, fmt.Errorf("Beeper AI is not available")
	}
	if provider, ok := cl.Main.providerForLogin(cl.UserLogin, aiid.DefaultProvider); ok && provider.BaseURL != "" {
		return provider, nil
	}
	if cl.UserLogin.UserLogin == nil {
		return aiid.ProviderConfig{}, fmt.Errorf("Beeper AI is not available")
	}
	provider := cl.Main.defaultProviderConfig(cl.UserLogin.UserMXID)
	if provider.BaseURL == "" {
		return aiid.ProviderConfig{}, fmt.Errorf("Beeper AI is not available for %s", cl.UserLogin.UserMXID.Homeserver())
	}
	return provider, nil
}

func aiServicesLimitsURL(proxyBaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(normalizeResponsesBaseURL(proxyBaseURL), "/"))
	if err != nil {
		return "", err
	}
	parsed.Path = trimAIProxyProviderPath(parsed.Path)
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/limits"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func formatLimitsCommandInfo(limits aiServicesLimitsResponse, now time.Time) string {
	var text strings.Builder
	text.WriteString("AI limits\n\n")
	appendLimitSection(&text, "Models", limits.Windows.LLM, now)
	text.WriteString("\n")
	appendLimitSection(&text, "Web Search", limits.Windows.WebTools, now)
	if !emptyLimitWindows(limits.Windows.AudioTranscriptions) {
		text.WriteString("\n")
		appendLimitSectionWithUsedFormatter(&text, "Transcription", limits.Windows.AudioTranscriptions, now, formatLimitUsedMinutes)
	}
	if !emptyLimitWindows(limits.Windows.AudioGeneration) {
		text.WriteString("\n")
		appendLimitSectionWithUsedFormatter(&text, "Audio Generation", limits.Windows.AudioGeneration, now, formatLimitUsedCharacters)
	}
	return text.String()
}

func formatRawLimitsCommandInfo(limits aiServicesLimitsResponse, now time.Time) string {
	var text strings.Builder
	text.WriteString("AI limits raw:")
	for _, category := range limitCategories(limits) {
		appendRawLimitCategory(&text, category.label, category.windows, now)
	}
	return text.String()
}

type limitCategory struct {
	label   string
	windows aiServicesLimitWindows
}

func limitCategories(limits aiServicesLimitsResponse) []limitCategory {
	categories := []limitCategory{{label: "LLM tokens", windows: limits.Windows.LLM}}
	if !emptyLimitWindows(limits.Windows.WebTools) {
		categories = append(categories, limitCategory{label: "Web tools", windows: limits.Windows.WebTools})
	}
	if !emptyLimitWindows(limits.Windows.AudioTranscriptions) {
		categories = append(categories, limitCategory{label: "Audio transcription seconds", windows: limits.Windows.AudioTranscriptions})
	}
	if !emptyLimitWindows(limits.Windows.AudioGeneration) {
		categories = append(categories, limitCategory{label: "Audio generation characters", windows: limits.Windows.AudioGeneration})
	}
	return categories
}

func appendLimitSection(text *strings.Builder, label string, windows aiServicesLimitWindows, now time.Time) {
	appendLimitSectionWithUsedFormatter(text, label, windows, now, formatLimitUsed)
}

func appendLimitSectionWithUsedFormatter(text *strings.Builder, label string, windows aiServicesLimitWindows, now time.Time, formatUsed func(aiServicesLimitWindow) string) {
	fmt.Fprintf(text, "## %s\n\n", label)
	if emptyLimitWindows(windows) {
		text.WriteString("No limits reported.\n")
		return
	}
	text.WriteString("| Window | Left | Used | Reset |\n")
	text.WriteString("| --- | ---: | ---: | --- |\n")
	appendLimitWindowSummaryWithUsedFormatter(text, "Daily", windows.Day, now, formatUsed)
	appendLimitWindowSummaryWithUsedFormatter(text, "Weekly", windows.Week, now, formatUsed)
	appendLimitWindowSummaryWithUsedFormatter(text, "Monthly", windows.Month, now, formatUsed)
}

func appendLimitWindowSummary(text *strings.Builder, windowName string, window aiServicesLimitWindow, now time.Time) {
	appendLimitWindowSummaryWithUsedFormatter(text, windowName, window, now, formatLimitUsed)
}

func appendLimitWindowSummaryWithUsedFormatter(text *strings.Builder, windowName string, window aiServicesLimitWindow, now time.Time, formatUsed func(aiServicesLimitWindow) string) {
	fmt.Fprintf(
		text,
		"| %s | %s | %s | %s |\n",
		windowName,
		formatLimitLeft(window),
		formatUsed(window),
		formatLimitReset(window, now),
	)
}

func formatLimitLeft(window aiServicesLimitWindow) string {
	if window.Limit < 0 {
		return "Unlimited"
	}
	if window.PercentageLeft <= 0 {
		return "**Out**"
	}
	return fmt.Sprintf("`%d%%`", window.PercentageLeft)
}

func formatLimitUsed(window aiServicesLimitWindow) string {
	if window.Limit == 0 && window.Used == 0 && window.Remaining == 0 {
		return "Not reported"
	}
	if window.Limit < 0 {
		return fmt.Sprintf("`%s` used", formatInt(window.Used))
	}
	return fmt.Sprintf("`%s / %s`", formatInt(window.Used), formatInt(window.Limit))
}

func formatLimitUsedMinutes(window aiServicesLimitWindow) string {
	if window.Limit == 0 && window.Used == 0 && window.Remaining == 0 {
		return "Not reported"
	}
	if window.Limit < 0 {
		return fmt.Sprintf("`%s` used", formatSecondsAsMinutes(window.Used))
	}
	return fmt.Sprintf("`%s / %s`", formatSecondsAsMinutes(window.Used), formatSecondsAsMinutes(window.Limit))
}

func formatLimitUsedCharacters(window aiServicesLimitWindow) string {
	if window.Limit == 0 && window.Used == 0 && window.Remaining == 0 {
		return "Not reported"
	}
	if window.Limit < 0 {
		return fmt.Sprintf("`%s chars` used", formatInt(window.Used))
	}
	return fmt.Sprintf("`%s / %s chars`", formatInt(window.Used), formatInt(window.Limit))
}

func formatSecondsAsMinutes(seconds int64) string {
	minutes := (seconds + 59) / 60
	return fmt.Sprintf("%s %s", formatInt(minutes), pluralize("minute", int(minutes)))
}

func formatLimitReset(window aiServicesLimitWindow, now time.Time) string {
	if window.ResetAtMS <= 0 {
		return "unknown"
	}
	return "in " + formatResetIn(time.UnixMilli(window.ResetAtMS), now)
}

func appendRawLimitCategory(text *strings.Builder, label string, windows aiServicesLimitWindows, now time.Time) {
	fmt.Fprintf(text, "\n\n%s:", label)
	appendRawLimitWindow(text, "Day", windows.Day, now)
	appendRawLimitWindow(text, "Week", windows.Week, now)
	appendRawLimitWindow(text, "Month", windows.Month, now)
}

func appendRawLimitWindow(text *strings.Builder, label string, window aiServicesLimitWindow, now time.Time) {
	fmt.Fprintf(
		text,
		"\n- %s: percentage_left=`%d`, limit=`%s`, used=`%s`, remaining=`%s`",
		label,
		window.PercentageLeft,
		formatInt(window.Limit),
		formatInt(window.Used),
		formatInt(window.Remaining),
	)
	if window.ResetAtMS > 0 {
		resetAt := time.UnixMilli(window.ResetAtMS)
		fmt.Fprintf(text, ", reset_at=`%d` (`%s`, in %s)", window.ResetAtMS, resetAt.UTC().Format(time.RFC3339), formatResetIn(resetAt, now))
	}
}

func sharedResetAt(categories []limitCategory) (time.Time, bool) {
	var shared time.Time
	for _, category := range categories {
		for _, window := range []aiServicesLimitWindow{category.windows.Day, category.windows.Week, category.windows.Month} {
			if window.ResetAtMS <= 0 {
				return time.Time{}, false
			}
			resetAt := time.UnixMilli(window.ResetAtMS)
			if shared.IsZero() {
				shared = resetAt
				continue
			}
			if !shared.Equal(resetAt) {
				return time.Time{}, false
			}
		}
	}
	return shared, !shared.IsZero()
}

func formatResetIn(resetAt time.Time, now time.Time) string {
	if !resetAt.After(now) {
		return "now"
	}
	totalMinutes := int(resetAt.Sub(now) / time.Minute)
	if totalMinutes < 1 {
		return "less than 1 minute"
	}
	days := totalMinutes / (24 * 60)
	totalMinutes %= 24 * 60
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", days, pluralize("day", days)))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", hours, pluralize("hour", hours)))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", minutes, pluralize("minute", minutes)))
	}
	return strings.Join(parts, " ")
}

func pluralize(word string, value int) string {
	if value == 1 {
		return word
	}
	return word + "s"
}

func joinSentence(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

func emptyLimitWindows(windows aiServicesLimitWindows) bool {
	return windows.Day == (aiServicesLimitWindow{}) &&
		windows.Week == (aiServicesLimitWindow{}) &&
		windows.Month == (aiServicesLimitWindow{})
}

func formatInt(value int64) string {
	text := strconv.FormatInt(value, 10)
	if len(text) <= 3 {
		return text
	}
	var out []byte
	first := len(text) % 3
	if first == 0 {
		first = 3
	}
	out = append(out, text[:first]...)
	for i := first; i < len(text); i += 3 {
		out = append(out, ',')
		out = append(out, text[i:i+3]...)
	}
	return string(out)
}
