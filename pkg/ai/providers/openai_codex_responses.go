package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
)

const defaultCodexBaseURL = "https://chatgpt.com/backend-api"
const jwtClaimPath = "https://api.openai.com/auth"
const openAIBetaResponsesWebSockets = "responses_websockets=2026-02-06"
const maxCodexRetries = 3
const baseCodexRetryDelay = time.Second

type OpenAICodexResponsesOptions struct {
	OpenAIResponsesOptions
	ReasoningSummary *string
	ServiceTier      string
	TextVerbosity    *string
}

type OpenAICodexWebSocketDebugStats struct {
	Requests                int    `json:"requests"`
	ConnectionsCreated      int    `json:"connectionsCreated"`
	ConnectionsReused       int    `json:"connectionsReused"`
	CachedContextRequests   int    `json:"cachedContextRequests"`
	StoreTrueRequests       int    `json:"storeTrueRequests"`
	FullContextRequests     int    `json:"fullContextRequests"`
	DeltaRequests           int    `json:"deltaRequests"`
	LastInputItems          int    `json:"lastInputItems"`
	LastDeltaInputItems     *int   `json:"lastDeltaInputItems,omitempty"`
	LastPreviousResponseID  string `json:"lastPreviousResponseId,omitempty"`
	WebSocketFailures       int    `json:"websocketFailures"`
	SSEFallbacks            int    `json:"sseFallbacks"`
	WebSocketFallbackActive *bool  `json:"websocketFallbackActive,omitempty"`
	LastWebSocketError      string `json:"lastWebSocketError,omitempty"`
}

type CachedCodexWebSocketContinuationState struct {
	LastRequestBody   map[string]any   `json:"lastRequestBody"`
	LastResponseID    string           `json:"lastResponseId"`
	LastResponseItems []map[string]any `json:"lastResponseItems"`
}

type CachedCodexWebSocketConnection struct {
	Busy         bool
	Continuation *CachedCodexWebSocketContinuationState
	Conn         *websocket.Conn
	IdleTimer    *time.Timer
}

type codexAPIError struct {
	Message string
}

func (err codexAPIError) Error() string {
	return err.Message
}

type codexProtocolError struct {
	Message string
}

func (err codexProtocolError) Error() string {
	return err.Message
}

var codexWebSocketDebugMu sync.Mutex
var codexWebSocketDebugStats = map[string]*OpenAICodexWebSocketDebugStats{}
var codexWebSocketSSEFallbackSessions = map[string]bool{}
var codexWebSocketSessionCache = map[string]*CachedCodexWebSocketConnection{}

func init() {
	ai.RegisterSessionResourceCleanup(func(sessionID ...string) error {
		CloseOpenAICodexWebSocketSessions(sessionID...)
		return nil
	})
}

func StreamSimpleOpenAICodexResponses(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = getEnvAPIKey(model.Provider)
	}
	if apiKey == "" {
		stream := ai.NewAssistantMessageEventStream()
		go pushError(stream, model, "No API key for provider: "+string(model.Provider))
		return stream
	}
	base := BuildBaseOptions(model, &options, apiKey)
	var reasoningEffort *ai.ThinkingLevel
	if options.Reasoning != nil {
		clamped := ai.ClampThinkingLevel(model, ai.ModelThinkingLevel(*options.Reasoning))
		if clamped != ai.ModelThinkingLevelOff {
			level := ai.ThinkingLevel(clamped)
			reasoningEffort = &level
		}
	}
	return StreamOpenAICodexResponses(ctx, model, llmContext, OpenAICodexResponsesOptions{
		OpenAIResponsesOptions: OpenAIResponsesOptions{StreamOptions: base, ReasoningEffort: reasoningEffort},
	})
}

func StreamOpenAICodexResponses(ctx context.Context, model ai.Model, llmContext ai.Context, options OpenAICodexResponsesOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		output := newAssistant(model)
		apiKey := options.APIKey
		if apiKey == "" {
			apiKey = getEnvAPIKey(model.Provider)
		}
		if apiKey == "" {
			pushFinalError(stream, &output, "No API key for provider: "+string(model.Provider))
			return
		}
		accountID, err := ExtractCodexAccountID(apiKey)
		if err != nil {
			pushFinalError(stream, &output, err.Error())
			return
		}
		body := BuildCodexRequestBody(model, llmContext, options)
		if options.OnPayload != nil {
			if next, ok, err := options.OnPayload(body, model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			} else if ok {
				if nextBody, ok := next.(map[string]any); ok {
					body = nextBody
				} else {
					pushFinalError(stream, &output, "onPayload returned unsupported Codex request body")
					return
				}
			}
		}
		rawBody, err := json.Marshal(body)
		if err != nil {
			pushFinalError(stream, &output, err.Error())
			return
		}
		webSocketFallbackActive := options.Transport != ai.TransportSSE && isCodexWebSocketSSEFallbackActive(options.SessionID)
		if webSocketFallbackActive {
			recordCodexSSEFallback(options.SessionID)
		}
		if options.Transport != ai.TransportSSE && !webSocketFallbackActive {
			started := false
			wsHeaders := BuildCodexWebSocketHeaders(model.Headers, options.Headers, accountID, apiKey, codexRequestID(options.SessionID))
			if err := processCodexWebSocketStream(ctx, ResolveCodexWebSocketURL(model.BaseURL), body, wsHeaders, &output, stream, model, func() { started = true }, options); err == nil {
				stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: output.StopReason, Message: &output})
				return
			} else {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || isCodexNonTransportError(err) {
					pushFinalError(stream, &output, err.Error())
					return
				}
				recordCodexWebSocketFailure(options.SessionID, err)
				if started {
					pushFinalError(stream, &output, err.Error())
					return
				}
				aiutils.AppendAssistantMessageDiagnostic(&output, aiutils.CreateAssistantMessageDiagnostic("provider_transport_failure", err, map[string]interface{}{
					"configuredTransport": options.Transport,
					"fallbackTransport":   "sse",
					"eventsEmitted":       false,
					"phase":               "before_message_stream_start",
					"requestBytes":        len(rawBody),
				}))
				recordCodexSSEFallback(options.SessionID)
			}
		}
		headers := BuildCodexSSEHeaders(model.Headers, options.Headers, accountID, apiKey, options.SessionID)
		response, err := doCodexSSERequest(ctx, ResolveCodexURL(model.BaseURL), headers, rawBody, options.StreamOptions)
		if err != nil {
			pushFinalError(stream, &output, err.Error())
			return
		}
		defer response.Body.Close()
		if options.OnResponse != nil {
			if err := options.OnResponse(providerResponse(response), model); err != nil {
				pushFinalError(stream, &output, err.Error())
				return
			}
		}
		stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: &output})
		state := newResponsesStreamState()
		processorOptions := options.OpenAIResponsesOptions
		for event := range parseCodexSSE(ctx, response.Body, stream, &output) {
			normalized, done := mapCodexEvent(event)
			if normalized == nil {
				if done {
					break
				}
				continue
			}
			state.apply(stream, &output, model, processorOptions, normalized)
			if done {
				break
			}
		}
		finishResponsesStream(stream, &output, state)
	}()
	return stream
}

func GetOpenAICodexWebSocketDebugStats(sessionID string) (*OpenAICodexWebSocketDebugStats, bool) {
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	stats, ok := codexWebSocketDebugStats[sessionID]
	if !ok {
		return nil, false
	}
	copy := *stats
	if stats.LastDeltaInputItems != nil {
		value := *stats.LastDeltaInputItems
		copy.LastDeltaInputItems = &value
	}
	if stats.WebSocketFallbackActive != nil {
		value := *stats.WebSocketFallbackActive
		copy.WebSocketFallbackActive = &value
	}
	return &copy, true
}

func ResetOpenAICodexWebSocketDebugStats(sessionID ...string) {
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	if len(sessionID) > 0 && sessionID[0] != "" {
		delete(codexWebSocketDebugStats, sessionID[0])
		delete(codexWebSocketSSEFallbackSessions, sessionID[0])
		return
	}
	codexWebSocketDebugStats = map[string]*OpenAICodexWebSocketDebugStats{}
	codexWebSocketSSEFallbackSessions = map[string]bool{}
}

func isCodexNonTransportError(err error) bool {
	var apiErr codexAPIError
	if errors.As(err, &apiErr) {
		return true
	}
	var protocolErr codexProtocolError
	return errors.As(err, &protocolErr)
}

func CloseOpenAICodexWebSocketSessions(sessionID ...string) {
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	if len(sessionID) > 0 && sessionID[0] != "" {
		closeCodexWebSocketEntryLocked(sessionID[0], codexWebSocketSessionCache[sessionID[0]])
		delete(codexWebSocketSessionCache, sessionID[0])
		return
	}
	for id, entry := range codexWebSocketSessionCache {
		closeCodexWebSocketEntryLocked(id, entry)
	}
	codexWebSocketSessionCache = map[string]*CachedCodexWebSocketConnection{}
}

func getOrCreateCodexWebSocketDebugStatsLocked(sessionID string) *OpenAICodexWebSocketDebugStats {
	stats := codexWebSocketDebugStats[sessionID]
	if stats == nil {
		stats = &OpenAICodexWebSocketDebugStats{}
		codexWebSocketDebugStats[sessionID] = stats
	}
	return stats
}

func isCodexWebSocketSSEFallbackActive(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	return codexWebSocketSSEFallbackSessions[sessionID]
}

func recordCodexSSEFallback(sessionID string) {
	if sessionID == "" {
		return
	}
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	stats := getOrCreateCodexWebSocketDebugStatsLocked(sessionID)
	stats.SSEFallbacks++
	active := codexWebSocketSSEFallbackSessions[sessionID]
	stats.WebSocketFallbackActive = &active
}

func recordCodexWebSocketFailure(sessionID string, err error) {
	if sessionID == "" {
		return
	}
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	codexWebSocketSSEFallbackSessions[sessionID] = true
	stats := getOrCreateCodexWebSocketDebugStatsLocked(sessionID)
	stats.WebSocketFailures++
	if err != nil {
		stats.LastWebSocketError = err.Error()
	}
	active := true
	stats.WebSocketFallbackActive = &active
}

func processCodexWebSocketStream(ctx context.Context, endpoint string, body map[string]any, headers map[string]string, output *ai.Message, stream *ai.AssistantMessageEventStream, model ai.Model, onStart func(), options OpenAICodexResponsesOptions) error {
	entry, reused, release, err := acquireCodexWebSocket(ctx, endpoint, headers, options.SessionID, options.StreamOptions)
	if err != nil {
		return err
	}
	keepConnection := true
	defer func() { release(keepConnection) }()

	useCachedContext := options.Transport == ai.TransportWebSocketCached || options.Transport == ai.TransportAuto || options.Transport == ""
	fullBody := body
	requestBody := fullBody
	if useCachedContext {
		requestBody = BuildCachedCodexWebSocketRequestBody(entry, fullBody)
	}
	recordCodexWebSocketRequestStats(options.SessionID, reused, useCachedContext, requestBody)

	payload := cloneMap(requestBody)
	payload["type"] = "response.create"
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		keepConnection = false
		return err
	}
	if err := entry.Conn.Write(ctx, websocket.MessageText, rawPayload); err != nil {
		keepConnection = false
		return err
	}

	started := false
	state := newResponsesStreamState()
	processorOptions := options.OpenAIResponsesOptions
	for {
		_, raw, err := entry.Conn.Read(ctx)
		if err != nil {
			keepConnection = false
			return err
		}
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			keepConnection = false
			return codexProtocolError{Message: fmt.Sprintf("Invalid Codex WebSocket JSON: %v", err)}
		}
		if !started {
			started = true
			onStart()
			stream.Push(ai.AssistantMessageEvent{Type: "start", Partial: output})
		}
		normalized, done := mapCodexEvent(event)
		if normalized != nil {
			state.apply(stream, output, model, processorOptions, normalized)
		}
		if done {
			break
		}
	}
	output.Content = state.blocks
	if output.StopReason == ai.StopReasonError && output.ErrorMessage != "" {
		entry.Continuation = nil
		keepConnection = false
		return codexAPIError{Message: output.ErrorMessage}
	}
	if useCachedContext && output.ResponseID != "" {
		responseItems := ConvertResponsesMessages(model, ai.Context{Messages: []ai.Message{*output}}, ConvertResponsesMessagesOptions{IncludeSystemPrompt: boolPtr(false)})
		filtered := make([]map[string]any, 0, len(responseItems))
		for _, item := range responseItems {
			if item["type"] != "function_call_output" {
				filtered = append(filtered, item)
			}
		}
		entry.Continuation = &CachedCodexWebSocketContinuationState{LastRequestBody: fullBody, LastResponseID: output.ResponseID, LastResponseItems: filtered}
	}
	return nil
}

func acquireCodexWebSocket(ctx context.Context, endpoint string, headers map[string]string, sessionID string, options ai.StreamOptions) (*CachedCodexWebSocketConnection, bool, func(bool), error) {
	if sessionID != "" {
		codexWebSocketDebugMu.Lock()
		if cached := codexWebSocketSessionCache[sessionID]; cached != nil && !cached.Busy && cached.Conn != nil {
			if cached.IdleTimer != nil {
				cached.IdleTimer.Stop()
				cached.IdleTimer = nil
			}
			cached.Busy = true
			codexWebSocketDebugMu.Unlock()
			return cached, true, func(keep bool) { releaseCodexWebSocket(sessionID, cached, keep) }, nil
		}
		codexWebSocketDebugMu.Unlock()
	}
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: codexHTTPHeader(headers), HTTPClient: codexWebSocketHTTPClient(options)})
	if err != nil {
		return nil, false, nil, err
	}
	entry := &CachedCodexWebSocketConnection{Busy: true, Conn: conn}
	if sessionID != "" {
		codexWebSocketDebugMu.Lock()
		codexWebSocketSessionCache[sessionID] = entry
		codexWebSocketDebugMu.Unlock()
	}
	return entry, false, func(keep bool) { releaseCodexWebSocket(sessionID, entry, keep) }, nil
}

func releaseCodexWebSocket(sessionID string, entry *CachedCodexWebSocketConnection, keep bool) {
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	if !keep || sessionID == "" || entry.Conn == nil {
		closeCodexWebSocketEntryLocked(sessionID, entry)
		if sessionID != "" && codexWebSocketSessionCache[sessionID] == entry {
			delete(codexWebSocketSessionCache, sessionID)
		}
		return
	}
	entry.Busy = false
	if entry.IdleTimer != nil {
		entry.IdleTimer.Stop()
	}
	entry.IdleTimer = time.AfterFunc(5*time.Minute, func() {
		codexWebSocketDebugMu.Lock()
		defer codexWebSocketDebugMu.Unlock()
		if entry.Busy {
			return
		}
		closeCodexWebSocketEntryLocked(sessionID, entry)
		if codexWebSocketSessionCache[sessionID] == entry {
			delete(codexWebSocketSessionCache, sessionID)
		}
	})
}

func closeCodexWebSocketEntryLocked(_ string, entry *CachedCodexWebSocketConnection) {
	if entry == nil {
		return
	}
	if entry.IdleTimer != nil {
		entry.IdleTimer.Stop()
		entry.IdleTimer = nil
	}
	if entry.Conn != nil {
		_ = entry.Conn.Close(websocket.StatusNormalClosure, "done")
		entry.Conn = nil
	}
	entry.Busy = false
}

func recordCodexWebSocketRequestStats(sessionID string, reused bool, useCachedContext bool, requestBody map[string]any) {
	if sessionID == "" {
		return
	}
	codexWebSocketDebugMu.Lock()
	defer codexWebSocketDebugMu.Unlock()
	stats := getOrCreateCodexWebSocketDebugStatsLocked(sessionID)
	stats.Requests++
	if reused {
		stats.ConnectionsReused++
	} else {
		stats.ConnectionsCreated++
	}
	if useCachedContext {
		stats.CachedContextRequests++
	}
	if store, ok := requestBody["store"].(bool); ok && store {
		stats.StoreTrueRequests++
	}
	stats.LastInputItems = len(codexInputItems(requestBody["input"]))
	if previousResponseID, ok := requestBody["previous_response_id"].(string); ok && previousResponseID != "" {
		stats.DeltaRequests++
		value := stats.LastInputItems
		stats.LastDeltaInputItems = &value
		stats.LastPreviousResponseID = previousResponseID
	} else {
		stats.FullContextRequests++
		stats.LastDeltaInputItems = nil
		stats.LastPreviousResponseID = ""
	}
}

func BuildCodexRequestBody(model ai.Model, llmContext ai.Context, options OpenAICodexResponsesOptions) map[string]any {
	includeSystemPrompt := false
	body := map[string]any{
		"model":               model.ID,
		"store":               false,
		"stream":              true,
		"instructions":        llmContext.SystemPrompt,
		"input":               ConvertResponsesMessages(model, llmContext, ConvertResponsesMessagesOptions{IncludeSystemPrompt: &includeSystemPrompt}),
		"text":                map[string]any{"verbosity": codexTextVerbosity(options.TextVerbosity)},
		"include":             []string{"reasoning.encrypted_content"},
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
	}
	if body["instructions"] == "" {
		body["instructions"] = "You are a helpful assistant."
	}
	if options.SessionID != "" {
		body["prompt_cache_key"] = ClampOpenAIPromptCacheKey(options.SessionID)
	}
	if options.Temperature != nil {
		body["temperature"] = *options.Temperature
	}
	if options.ServiceTier != "" {
		body["service_tier"] = options.ServiceTier
	}
	if len(llmContext.Tools) > 0 {
		body["tools"] = ConvertResponsesTools(llmContext.Tools, ConvertResponsesToolsOptions{StrictNull: true})
	}
	if options.ReasoningEffort != nil {
		effort := mappedThinkingLevel(model, *options.ReasoningEffort)
		if *options.ReasoningEffort == "none" {
			effort = mappedOffThinkingLevel(model)
		}
		if effort != "" {
			summary := "auto"
			if options.ReasoningSummary != nil {
				summary = *options.ReasoningSummary
			}
			body["reasoning"] = map[string]any{"effort": effort, "summary": summary}
		}
	}
	return body
}

func BuildCachedCodexWebSocketRequestBody(entry *CachedCodexWebSocketConnection, body map[string]any) map[string]any {
	if entry == nil || entry.Continuation == nil {
		return body
	}
	delta, ok := getCachedCodexWebSocketInputDelta(body, entry.Continuation)
	if !ok || entry.Continuation.LastResponseID == "" {
		entry.Continuation = nil
		return body
	}
	next := cloneMap(body)
	next["previous_response_id"] = entry.Continuation.LastResponseID
	next["input"] = delta
	return next
}

func getCachedCodexWebSocketInputDelta(body map[string]any, continuation *CachedCodexWebSocketContinuationState) ([]map[string]any, bool) {
	if continuation == nil || !codexRequestBodiesMatchExceptInput(body, continuation.LastRequestBody) {
		return nil, false
	}
	currentInput := codexInputItems(body["input"])
	baseline := append(codexInputItems(continuation.LastRequestBody["input"]), continuation.LastResponseItems...)
	if len(currentInput) < len(baseline) {
		return nil, false
	}
	if !codexInputsEqual(currentInput[:len(baseline)], baseline) {
		return nil, false
	}
	return currentInput[len(baseline):], true
}

func codexRequestBodiesMatchExceptInput(a map[string]any, b map[string]any) bool {
	return jsonStableEqual(codexRequestBodyWithoutInput(a), codexRequestBodyWithoutInput(b))
}

func codexRequestBodyWithoutInput(body map[string]any) map[string]any {
	out := cloneMap(body)
	delete(out, "input")
	delete(out, "previous_response_id")
	return out
}

func codexInputsEqual(a []map[string]any, b []map[string]any) bool {
	return jsonStableEqual(a, b)
}

func codexInputItems(value any) []map[string]any {
	switch input := value.(type) {
	case []map[string]any:
		return input
	case []any:
		out := make([]map[string]any, 0, len(input))
		for _, item := range input {
			if itemMap, ok := item.(map[string]any); ok {
				out = append(out, itemMap)
			}
		}
		return out
	default:
		return nil
	}
}

func cloneMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func jsonStableEqual(a any, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func ResolveCodexURL(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		raw = defaultCodexBaseURL
	}
	normalized := strings.TrimRight(raw, "/")
	if strings.HasSuffix(normalized, "/codex/responses") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/codex") {
		return normalized + "/responses"
	}
	return normalized + "/codex/responses"
}

func ResolveCodexWebSocketURL(baseURL string) string {
	resolved := ResolveCodexURL(baseURL)
	parsed, err := url.Parse(resolved)
	if err != nil {
		return resolved
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else if parsed.Scheme == "http" {
		parsed.Scheme = "ws"
	}
	return parsed.String()
}

func ExtractCodexAccountID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("failed to extract accountId from token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("failed to extract accountId from token")
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("failed to extract accountId from token")
	}
	auth, ok := claims[jwtClaimPath].(map[string]any)
	if !ok {
		return "", errors.New("failed to extract accountId from token")
	}
	accountID, ok := auth["chatgpt_account_id"].(string)
	if !ok || accountID == "" {
		return "", errors.New("failed to extract accountId from token")
	}
	return accountID, nil
}

func BuildCodexSSEHeaders(modelHeaders map[string]string, additionalHeaders map[string]string, accountID string, token string, sessionID string) map[string]string {
	headers := buildBaseCodexHeaders(modelHeaders, additionalHeaders, accountID, token)
	headers["OpenAI-Beta"] = "responses=experimental"
	headers["accept"] = "text/event-stream"
	headers["content-type"] = "application/json"
	if sessionID != "" {
		headers["session_id"] = sessionID
		headers["x-client-request-id"] = sessionID
	}
	return headers
}

func BuildCodexWebSocketHeaders(modelHeaders map[string]string, additionalHeaders map[string]string, accountID string, token string, requestID string) map[string]string {
	headers := buildBaseCodexHeaders(modelHeaders, additionalHeaders, accountID, token)
	delete(headers, "accept")
	delete(headers, "content-type")
	delete(headers, "OpenAI-Beta")
	delete(headers, "openai-beta")
	headers["OpenAI-Beta"] = openAIBetaResponsesWebSockets
	headers["x-client-request-id"] = requestID
	headers["session_id"] = requestID
	return headers
}

func codexRequestID(sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "codex_" + fmt.Sprint(time.Now().UnixMilli()) + "_" + hex.EncodeToString(random[:])
	}
	return "codex_" + fmt.Sprint(time.Now().UnixMilli())
}

func GetCodexServiceTierCostMultiplier(model ai.Model, serviceTier string) float64 {
	switch serviceTier {
	case "flex":
		return 0.5
	case "priority":
		if model.ID == "gpt-5.5" {
			return 2.5
		}
		return 2
	default:
		return 1
	}
}

func ApplyCodexServiceTierPricing(usage *ai.Usage, serviceTier string, model ai.Model) {
	multiplier := GetCodexServiceTierCostMultiplier(model, serviceTier)
	if multiplier == 1 {
		return
	}
	usage.Cost.Input *= multiplier
	usage.Cost.Output *= multiplier
	usage.Cost.CacheRead *= multiplier
	usage.Cost.CacheWrite *= multiplier
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}

func ResolveCodexServiceTier(responseServiceTier string, requestServiceTier string) string {
	if responseServiceTier == "default" && (requestServiceTier == "flex" || requestServiceTier == "priority") {
		return requestServiceTier
	}
	if responseServiceTier != "" {
		return responseServiceTier
	}
	return requestServiceTier
}

func codexHTTPHeader(headers map[string]string) http.Header {
	out := http.Header{}
	for key, value := range headers {
		out.Set(key, value)
	}
	return out
}

func codexWebSocketHTTPClient(options ai.StreamOptions) *http.Client {
	client := &http.Client{}
	if options.TimeoutMs != nil && *options.TimeoutMs > 0 {
		client.Timeout = time.Duration(*options.TimeoutMs) * time.Millisecond
	}
	return client
}

func boolPtr(value bool) *bool {
	return &value
}

func doCodexSSERequest(ctx context.Context, endpoint string, headers map[string]string, body []byte, options ai.StreamOptions) (*http.Response, error) {
	client := &http.Client{}
	if options.TimeoutMs != nil && *options.TimeoutMs > 0 {
		client.Timeout = time.Duration(*options.TimeoutMs) * time.Millisecond
	}
	maxRetries := maxCodexRetries
	if options.MaxRetries != nil {
		maxRetries = *options.MaxRetries
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := client.Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		if err != nil {
			lastErr = err
		} else {
			errorText, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if !isRetryableCodexError(response.StatusCode, string(errorText)) || attempt == maxRetries {
				return nil, fmt.Errorf("%s", codexErrorMessage(response.StatusCode, response.Status, string(errorText)))
			}
			lastErr = fmt.Errorf("%s", string(errorText))
		}
		if attempt < maxRetries {
			delay := codexRetryDelay(attempt, response, options.MaxRetryDelayMs)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, lastErr
}

func parseCodexSSE(ctx context.Context, body io.Reader, stream *ai.AssistantMessageEventStream, output *ai.Message) <-chan map[string]any {
	events := make(chan map[string]any)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		dataLines := []string{}
		flush := func() bool {
			if len(dataLines) == 0 {
				return true
			}
			data := strings.TrimSpace(strings.Join(dataLines, "\n"))
			dataLines = nil
			if data == "" || data == "[DONE]" {
				return true
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				output.StopReason = ai.StopReasonError
				output.ErrorMessage = "Invalid Codex SSE JSON: " + err.Error()
				return false
			}
			select {
			case <-ctx.Done():
				output.StopReason = ai.StopReasonAborted
				output.ErrorMessage = ctx.Err().Error()
				return false
			case events <- event:
				return true
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if !flush() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if err := scanner.Err(); err != nil {
			output.StopReason = ai.StopReasonError
			output.ErrorMessage = err.Error()
			return
		}
		_ = flush()
	}()
	return events
}

func mapCodexEvent(event map[string]any) (map[string]any, bool) {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "":
		return nil, false
	case "error":
		return event, true
	case "response.failed":
		return event, true
	case "response.done", "response.completed", "response.incomplete":
		if response, ok := event["response"].(map[string]any); ok {
			if status, ok := response["status"].(string); ok {
				response["status"] = normalizeCodexStatus(status)
			}
			event["response"] = response
		}
		event["type"] = "response.completed"
		return event, true
	default:
		return event, false
	}
}

func normalizeCodexStatus(status string) string {
	switch status {
	case "completed", "incomplete", "failed", "cancelled", "queued", "in_progress":
		return status
	default:
		return ""
	}
}

func isRetryableCodexError(status int, errorText string) bool {
	if status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout {
		return true
	}
	lower := strings.ToLower(errorText)
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "ratelimit") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "service unavailable") ||
		strings.Contains(lower, "upstream connect") ||
		strings.Contains(lower, "connection refused")
}

func codexRetryDelay(attempt int, response *http.Response, maxRetryDelayMs *int) time.Duration {
	delay := baseCodexRetryDelay * time.Duration(1<<attempt)
	if requested, ok := RequestedRetryDelay(response, time.Now()); ok {
		delay = requested
	}
	if maxRetryDelayMs != nil && *maxRetryDelayMs >= 0 {
		maxDelay := time.Duration(*maxRetryDelayMs) * time.Millisecond
		if delay > maxDelay {
			return maxDelay
		}
	}
	return delay
}

func codexErrorMessage(statusCode int, status string, raw string) string {
	message := strings.TrimSpace(raw)
	if message == "" {
		message = status
	}
	var parsed struct {
		Error struct {
			Code     string  `json:"code"`
			Type     string  `json:"type"`
			Message  string  `json:"message"`
			PlanType string  `json:"plan_type"`
			ResetsAt float64 `json:"resets_at"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &parsed) == nil {
		code := parsed.Error.Code
		if code == "" {
			code = parsed.Error.Type
		}
		if parsed.Error.Message != "" {
			message = parsed.Error.Message
		}
		lowerCode := strings.ToLower(code)
		if statusCode == http.StatusTooManyRequests || strings.Contains(lowerCode, "usage_limit_reached") || strings.Contains(lowerCode, "usage_not_included") || strings.Contains(lowerCode, "rate_limit_exceeded") {
			plan := ""
			if parsed.Error.PlanType != "" {
				plan = " (" + strings.ToLower(parsed.Error.PlanType) + " plan)"
			}
			when := ""
			if parsed.Error.ResetsAt > 0 {
				mins := int((parsed.Error.ResetsAt*1000 - float64(time.Now().UnixMilli()) + 30000) / 60000)
				if mins < 0 {
					mins = 0
				}
				when = fmt.Sprintf(" Try again in ~%d min.", mins)
			}
			return strings.TrimSpace("You have hit your ChatGPT usage limit" + plan + "." + when)
		}
	}
	return message
}

func codexTextVerbosity(value *string) string {
	if value == nil || *value == "" {
		return "low"
	}
	return *value
}

func buildBaseCodexHeaders(modelHeaders map[string]string, additionalHeaders map[string]string, accountID string, token string) map[string]string {
	headers := map[string]string{}
	for key, value := range modelHeaders {
		headers[key] = value
	}
	for key, value := range additionalHeaders {
		headers[key] = value
	}
	headers["Authorization"] = "Bearer " + token
	headers["chatgpt-account-id"] = accountID
	headers["originator"] = "pi"
	headers["User-Agent"] = "pi (go)"
	return headers
}
