package chattools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetSessionSchemaUsesArrayRequired(t *testing.T) {
	tool := GetSessionTool(SessionInfo{})
	raw, err := json.Marshal(tool.Tool.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"required":null`) {
		t.Fatalf("required must be an array, got %s", string(raw))
	}
	required, ok := tool.Tool.Parameters["required"].([]string)
	if !ok || len(required) != 0 {
		t.Fatalf("expected empty required array, got %#v", tool.Tool.Parameters["required"])
	}
}

func TestGetSessionReturnsFreshMetadata(t *testing.T) {
	tool := GetSessionTool(SessionInfo{
		RoomTitle:       "Room",
		SessionID:       "session-1",
		LoginID:         "login-1",
		ProviderID:      "beeper",
		ModelID:         "gpt-5",
		ReasoningLevel:  "low",
		DisabledTools:   []string{"web_search"},
		AttachmentCount: 1,
	})
	result, err := tool.Execute(context.Background(), "call", map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var info SessionInfo
	if err := json.Unmarshal([]byte(result.Content[0].Text), &info); err != nil {
		t.Fatal(err)
	}
	if info.Timestamp == "" || info.Timezone == "" || info.ThreadID != "session-1" {
		t.Fatalf("expected fresh session metadata, got %#v", info)
	}
}

func TestToolsOmitsDisabledSearch(t *testing.T) {
	tools := Tools(SessionInfo{}, FetchOptions{}, SearchOptions{Enabled: false})
	for _, tool := range tools {
		if tool.Tool.Name == "web_search" {
			t.Fatalf("web_search should not be exposed when disabled")
		}
	}
}

func TestFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>Hello</title><body><script>drop</script><p>Visible text</p></body></html>"))
	}))
	defer server.Close()

	result, err := Fetch(context.Background(), server.URL, FetchOptions{Timeout: time.Second, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 200 || result.Title != "Hello" || !strings.Contains(result.Text, "Visible text") || strings.Contains(result.Text, "drop") {
		t.Fatalf("unexpected fetch result %#v", result)
	}
}

func TestFetchUsesDirectFetchForAssetsWhenExaConfigured(t *testing.T) {
	exaHit := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "exa.test" {
			exaHit = true
			return testResponse(req, http.StatusOK, "application/json", `{"results":[]}`), nil
		}
		return testResponse(req, http.StatusOK, "text/markdown", "# Title\n\nBody"), nil
	})}

	result, err := Fetch(context.Background(), "https://example.com/doc.md", FetchOptions{Timeout: time.Second, ExaEndpoint: "https://exa.test/contents", Client: client, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if exaHit || result.FetchMethod != "direct" || result.Title != "" || !strings.Contains(result.Text, "Title Body") {
		t.Fatalf("unexpected direct fetch result %#v exaHit=%v", result, exaHit)
	}
}

func TestFetchUsesExaContentsForPages(t *testing.T) {
	exa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected request method/header")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		urls, ok := payload["urls"].([]any)
		if !ok || len(urls) != 1 || urls[0] != "https://example.com/page" {
			t.Fatalf("unexpected contents payload %#v", payload)
		}
		text, ok := payload["text"].(map[string]any)
		if !ok || text["maxCharacters"] != float64(100) || text["verbosity"] != "standard" {
			t.Fatalf("unexpected text payload %#v", payload)
		}
		_, _ = w.Write([]byte(`{"requestId":"req_1","costDollars":{"total":0.001},"statuses":[{"id":"https://example.com/page","status":"success","source":"crawled"}],"results":[{"id":"doc_1","title":"Page","url":"https://example.com/page","text":"Extracted page text","publishedDate":"2026-01-01","author":"A","highlights":["hit"],"summary":"sum","extras":{"links":["https://example.com/next"]}}]}`))
	}))
	defer exa.Close()

	result, err := Fetch(context.Background(), "https://example.com/page", FetchOptions{Timeout: time.Second, ExaEndpoint: exa.URL, APIKey: "key", MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.FetchMethod != "exa" || result.RequestID != "req_1" || result.Source != "crawled" || result.ID != "doc_1" || result.Title != "Page" || result.Text != "Extracted page text" {
		t.Fatalf("unexpected Exa fetch result %#v", result)
	}
	if result.Published != "2026-01-01" || result.Author != "A" || len(result.Highlights) != 1 || result.Summary != "sum" || result.Extras["links"] == nil {
		t.Fatalf("missing Exa fetch metadata %#v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "costDollars") || strings.Contains(string(raw), "fetch_method") {
		t.Fatalf("Exa internal metadata leaked into fetch JSON: %s", string(raw))
	}
}

func TestFetchFallsBackToDirectWhenExaFails(t *testing.T) {
	exaHit := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "exa.test" {
			exaHit = true
			return testResponse(req, http.StatusBadGateway, "text/plain", "nope"), nil
		}
		return testResponse(req, http.StatusOK, "text/html", "<html><title>Fallback</title><body>Direct page</body></html>"), nil
	})}

	result, err := Fetch(context.Background(), "https://example.com/page", FetchOptions{Timeout: time.Second, ExaEndpoint: "https://exa.test/contents", Client: client, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !exaHit || result.FetchMethod != "direct" || result.Title != "Fallback" || !strings.Contains(result.Text, "Direct page") {
		t.Fatalf("unexpected fallback result %#v exaHit=%v", result, exaHit)
	}
}

func TestFetchRejectsUnsupportedScheme(t *testing.T) {
	if _, err := Fetch(context.Background(), "file:///etc/passwd", FetchOptions{}); err == nil {
		t.Fatalf("expected unsupported scheme error")
	}
}

func TestSearchUsesConfiguredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected request method/header")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["query"] != "query" || payload["numResults"] != float64(5) {
			t.Fatalf("unexpected search payload %#v", payload)
		}
		if payload["useAutoprompt"] != false {
			t.Fatalf("unexpected search payload %#v", payload)
		}
		_, _ = w.Write([]byte(`{"requestId":"req_1","resolvedSearchType":"auto","costDollars":{"total":0.001},"output":{"content":"synth"},"results":[{"id":"doc_1","title":"One","url":"https://example.com","text":"ok","highlights":["hit"],"highlightScores":[0.5],"summary":"sum","publishedDate":"2026-01-01","siteName":"Example","author":"A","image":"https://example.com/image.png","favicon":"https://example.com/favicon.ico","subpages":[{"id":"sub_1","title":"Sub","url":"https://example.com/sub"}],"entities":[{"type":"company"}],"extras":{"links":["https://example.com/link"]}}]}`))
	}))
	defer server.Close()

	result, err := Search(context.Background(), "query", 5, SearchRequestOptions{}, SearchOptions{Enabled: true, Endpoint: server.URL, APIKey: "key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Query != "query" || result.RequestID != "req_1" || result.ResolvedSearchType != "auto" || result.Output["content"] != "synth" {
		t.Fatalf("missing top-level Exa metadata: %#v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "costDollars") {
		t.Fatalf("Exa cost metadata leaked into search JSON: %s", string(raw))
	}
	if len(result.Results) != 1 || result.Results[0].ID != "doc_1" || result.Results[0].Title != "One" || result.Results[0].Snippet != "hit" || result.Results[0].Text != "ok" {
		t.Fatalf("unexpected search result %#v", result)
	}
	if result.Results[0].Published != "2026-01-01" || result.Results[0].SiteName != "Example" || result.Results[0].Author != "A" {
		t.Fatalf("missing Exa metadata: %#v", result.Results[0])
	}
	if len(result.Results[0].Highlights) != 1 || result.Results[0].HighlightScores[0] != 0.5 || result.Results[0].Summary != "sum" {
		t.Fatalf("missing Exa content fields: %#v", result.Results[0])
	}
	if len(result.Results[0].Subpages) != 1 || len(result.Results[0].Entities) != 1 || result.Results[0].Extras["links"] == nil {
		t.Fatalf("missing Exa nested fields: %#v", result.Results[0])
	}
}

func TestSearchMapsToolOptionsToExaPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["type"] != "deep" || payload["category"] != "news" || payload["userLocation"] != "US" || payload["systemPrompt"] != "prefer official sources" {
			t.Fatalf("missing scalar options %#v", payload)
		}
		if domains, ok := payload["includeDomains"].([]any); !ok || len(domains) != 1 || domains[0] != "example.com" {
			t.Fatalf("missing includeDomains %#v", payload)
		}
		if moderation, ok := payload["moderation"].(bool); !ok || !moderation {
			t.Fatalf("missing moderation %#v", payload)
		}
		contents, ok := payload["contents"].(map[string]any)
		if !ok || contents["highlights"] != true {
			t.Fatalf("missing contents %#v", payload)
		}
		outputSchema, ok := payload["outputSchema"].(map[string]any)
		if !ok || outputSchema["type"] != "text" {
			t.Fatalf("missing outputSchema %#v", payload)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	tool := WebSearchTool(SearchOptions{Enabled: true, Endpoint: server.URL, Timeout: time.Second})
	_, err := tool.Execute(context.Background(), "call", map[string]any{
		"query":          "query",
		"limit":          3,
		"includeDomains": []any{"example.com"},
		"type":           "deep",
		"category":       "news",
		"userLocation":   "US",
		"moderation":     true,
		"contents":       map[string]any{"highlights": true},
		"outputSchema":   map[string]any{"type": "text"},
		"systemPrompt":   "prefer official sources",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchRequiresConfiguration(t *testing.T) {
	if _, err := Search(context.Background(), "query", 5, SearchRequestOptions{}, SearchOptions{}); err == nil {
		t.Fatalf("expected configuration error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testResponse(req *http.Request, status int, contentType string, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
