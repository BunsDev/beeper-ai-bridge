package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestStreamProxyReconstructsPartialMessage(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stream" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"type":"start"}`,
			`{"type":"text_start","contentIndex":0}`,
			`{"type":"text_delta","contentIndex":0,"delta":"hel"}`,
			`{"type":"text_delta","contentIndex":0,"delta":"lo"}`,
			`{"type":"text_end","contentIndex":0,"contentSignature":"sig"}`,
			`{"type":"toolcall_start","contentIndex":1,"id":"call_1","toolName":"read"}`,
			`{"type":"toolcall_delta","contentIndex":1,"delta":"{\"path\":\"x\"}"}`,
			`{"type":"toolcall_end","contentIndex":1}`,
			`{"type":"done","reason":"toolUse","usage":{"input":1,"output":2,"totalTokens":3,"cost":{}}}`,
		}
		for _, event := range events {
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
		}
	}))
	defer server.Close()

	maxTokens := 10
	stream := StreamProxy(t.Context(), ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai"}, ai.Context{SystemPrompt: "system"}, ProxyStreamOptions{
		SimpleStreamOptions: ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{MaxTokens: &maxTokens, SessionID: "session-1"}},
		AuthToken:           "token",
		ProxyURL:            server.URL,
	})
	var eventTypes []string
	for event := range stream.Events() {
		eventTypes = append(eventTypes, event.Type)
	}
	result := stream.Result()
	if result.StopReason != ai.StopReasonToolUse {
		t.Fatalf("expected toolUse stop, got %q", result.StopReason)
	}
	blocks := result.Content.([]ai.ContentBlock)
	if len(blocks) != 2 || blocks[0].Text != "hello" || blocks[0].TextSignature != "sig" {
		t.Fatalf("unexpected text block %#v", blocks)
	}
	if blocks[1].ID != "call_1" || blocks[1].Name != "read" || blocks[1].Arguments["path"] != "x" {
		t.Fatalf("unexpected tool block %#v", blocks[1])
	}
	if requestBody["model"].(map[string]any)["id"] != "gpt-test" {
		t.Fatalf("unexpected request model %#v", requestBody)
	}
	options := requestBody["options"].(map[string]any)
	if options["maxTokens"].(float64) != 10 || options["sessionId"].(string) != "session-1" {
		t.Fatalf("unexpected proxy options %#v", options)
	}
	if !containsEventType(eventTypes, "toolcall_end") || !containsEventType(eventTypes, "done") {
		t.Fatalf("missing expected events %#v", eventTypes)
	}
}

func TestStreamProxyHTTPErrorProducesErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer server.Close()

	stream := StreamProxy(t.Context(), ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai"}, ai.Context{}, ProxyStreamOptions{AuthToken: "token", ProxyURL: server.URL})
	result := stream.Result()
	if result.StopReason != ai.StopReasonError || result.ErrorMessage != "Proxy error: upstream down" {
		t.Fatalf("unexpected error result %#v", result)
	}
}

func containsEventType(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
