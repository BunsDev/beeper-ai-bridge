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
		ChatID:               "session-1",
		ChatTitle:            "Markdown Chaos Test",
		ChatFirstMessageAt:   "2026-05-31T22:00:00Z",
		SelectedModel:        "gpt-5",
		SelectedReasoning:    "low",
		DisabledTools:        []string{"web_search"},
		SearchMode:           "off",
		FetchMode:            "beeper",
		LastMessageTimestamp: "2026-05-31T22:34:00Z",
		LastKnownTimezone:    "Europe/Amsterdam",
	})
	result, err := tool.Execute(context.Background(), "call", map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var info SessionInfo
	if err := json.Unmarshal([]byte(result.Content[0].Text), &info); err != nil {
		t.Fatal(err)
	}
	if info.CurrentTimestamp == "" || info.LastMessageTimestamp != "2026-05-31T22:34:00Z" || info.LastKnownTimezone != "Europe/Amsterdam" || info.ChatID != "session-1" || info.ChatTitle != "Markdown Chaos Test" || info.SelectedModel != "gpt-5" || len(info.DisabledTools) != 1 || info.DisabledTools[0] != "web_search" || info.SearchMode != "off" || info.FetchMode != "beeper" {
		t.Fatalf("expected fresh session metadata, got %#v", info)
	}
	assertSessionKeys(t, result.Content[0].Text, "current_timestamp", "chat_id", "chat_title", "chat_first_message_at", "selected_model", "selected_reasoning", "disabled_tools", "search_mode", "fetch_mode", "last_message_timestamp", "last_known_timezone")
}

func TestGetSessionIncludesProfileOnlyWhenResolverReturnsIt(t *testing.T) {
	tool := GetSessionToolWithOptions(SessionInfo{ChatID: "session-1"}, SessionOptions{
		ResolveProfile: func(ctx context.Context, toolCallID string) (*SessionProfile, error) {
			if toolCallID != "call" {
				t.Fatalf("tool call ID = %q", toolCallID)
			}
			return &SessionProfile{Email: "user@example.com", Username: "user", MatrixProfile: map[string]any{"displayname": "User Name"}, GravatarProfile: map[string]any{"hash": "abc"}}, nil
		},
	})
	result, err := tool.Execute(context.Background(), "call", map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var info SessionInfo
	if err := json.Unmarshal([]byte(result.Content[0].Text), &info); err != nil {
		t.Fatal(err)
	}
	if info.BeeperAccountEmail != "user@example.com" || info.BeeperUsername != "user" || info.BeeperDisplayName != "User Name" || info.GravatarProfile == nil {
		t.Fatalf("missing approved profile: %#v", info)
	}
	assertSessionKeys(t, result.Content[0].Text, "current_timestamp", "chat_id", "beeper_username", "beeper_display_name", "beeper_account_email", "gravatar_profile", "last_message_timestamp")

	baseline := GetSessionToolWithOptions(SessionInfo{ChatID: "session-1"}, SessionOptions{
		ResolveProfile: func(ctx context.Context, toolCallID string) (*SessionProfile, error) {
			return nil, nil
		},
	})
	result, err = baseline.Execute(context.Background(), "call", map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content[0].Text, "beeper_profile") || strings.Contains(result.Content[0].Text, "beeper_account_email") {
		t.Fatalf("denied baseline session should not include profile fields: %s", result.Content[0].Text)
	}
}

func assertSessionKeys(t *testing.T, raw string, keys ...string) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, key := range keys {
		want[key] = true
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected session metadata keys: got %#v want %#v", got, want)
	}
	for key := range got {
		if !want[key] {
			t.Fatalf("unexpected session metadata key %q in %s", key, raw)
		}
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

func TestToolsOmitsDisabledFetch(t *testing.T) {
	tools := Tools(SessionInfo{}, FetchOptions{Disabled: true}, SearchOptions{Enabled: true})
	for _, tool := range tools {
		if tool.Tool.Name == "fetch" {
			t.Fatalf("fetch should not be exposed when disabled")
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

func TestFetchUsesDirectFetchForAssetsWhenToolEndpointConfigured(t *testing.T) {
	exaHit := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "exa.test" {
			exaHit = true
			return testResponse(req, http.StatusOK, "application/json", `{"results":[]}`), nil
		}
		if !strings.Contains(req.Header.Get("Accept"), "text/markdown") {
			t.Fatalf("direct fetch should prefer readable representations, got Accept=%q", req.Header.Get("Accept"))
		}
		return testResponse(req, http.StatusOK, "text/markdown", "# Title\n\nBody"), nil
	})}

	result, err := Fetch(context.Background(), "https://example.com/doc.md", FetchOptions{Timeout: time.Second, ToolEndpoint: "https://exa.test/contents", Client: client, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if exaHit || result.FetchMethod != "direct" || result.Title != "" || !strings.Contains(result.Text, "Title Body") {
		t.Fatalf("unexpected direct fetch result %#v exaHit=%v", result, exaHit)
	}
}

func TestFetchUsesMarkdownAlternateAfterToolEndpointFails(t *testing.T) {
	exaHit := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "exa.test" {
			exaHit = true
			return testResponse(req, http.StatusBadGateway, "text/plain", "nope"), nil
		}
		switch req.URL.Path {
		case "/page":
			resp := testResponse(req, http.StatusOK, "text/html; charset=utf-8", `<html><head><link rel="alternate" type="text/markdown" href="/page.md"></head><body>HTML page</body></html>`)
			resp.Header.Set("Link", `</from-header.md>; rel="alternate"; type="text/markdown"`)
			return resp, nil
		case "/from-header.md":
			return testResponse(req, http.StatusOK, "text/markdown", "# Header markdown"), nil
		default:
			return testResponse(req, http.StatusNotFound, "text/plain", "not found"), nil
		}
	})}

	result, err := Fetch(context.Background(), "https://example.com/page", FetchOptions{Timeout: time.Second, ToolEndpoint: "https://exa.test/contents", Client: client, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !exaHit || result.FetchMethod != "direct" || result.FinalURL != "https://example.com/from-header.md" || !strings.Contains(result.Markdown, "Header markdown") {
		t.Fatalf("unexpected alternate fetch result %#v exaHit=%v", result, exaHit)
	}
}

func TestFetchUsesToolEndpointForPages(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "exa.test" {
			return testResponse(req, http.StatusOK, "text/html", "<html><title>Page</title><body>HTML page</body></html>"), nil
		}
		if req.Method != http.MethodPost || req.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected request method/header")
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["url"] != "https://example.com/page" || payload["max_chars"] != float64(100) {
			t.Fatalf("unexpected fetch payload %#v", payload)
		}
		return testResponse(req, http.StatusOK, "application/json", `{"request_id":"req_1","title":"Page","description":"Page description","url":"https://example.com/page","final_url":"https://example.com/page","text":"Plain page text","markdown":"# Extracted page text","published_at":"2026-01-01","author":"A","favicon_url":"https://example.com/favicon.ico","metadata":{"links":["https://example.com/next"]}}`), nil
	})}

	result, err := Fetch(context.Background(), "https://example.com/page", FetchOptions{Timeout: time.Second, ToolEndpoint: "https://exa.test/contents", APIKey: "key", Client: client, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.FetchMethod != "web_tool" || result.RequestID != "req_1" || result.Title != "Page" || result.Text != "Plain page text" || result.Markdown != "# Extracted page text" {
		t.Fatalf("unexpected fetch result %#v", result)
	}
	if result.Description != "Page description" || result.Favicon != "https://example.com/favicon.ico" || result.FaviconURL != "https://example.com/favicon.ico" || result.Published != "2026-01-01" || result.Author != "A" || result.Extras["links"] == nil {
		t.Fatalf("missing fetch metadata %#v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "costDollars") || strings.Contains(string(raw), "fetch_method") {
		t.Fatalf("internal metadata leaked into fetch JSON: %s", string(raw))
	}
}

func TestFetchDirectExtractsHTMLSourceMetadata(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head><title>Direct Page</title><meta name="description" content="Direct page description"><link rel="icon" href="/favicon.ico"></head><body>Direct page text</body></html>`)
	}))
	defer page.Close()

	result, err := Fetch(context.Background(), page.URL+"/page", FetchOptions{Timeout: time.Second, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.FetchMethod != "direct" || result.Title != "Direct Page" || result.Description != "Direct page description" {
		t.Fatalf("missing direct fetch metadata %#v", result)
	}
	if result.Favicon != page.URL+"/favicon.ico" {
		t.Fatalf("expected resolved favicon, got %#v", result)
	}
}

func TestFetchFallsBackToDirectWhenToolEndpointFails(t *testing.T) {
	exaHit := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "exa.test" {
			exaHit = true
			return testResponse(req, http.StatusBadGateway, "text/plain", "nope"), nil
		}
		return testResponse(req, http.StatusOK, "text/html", "<html><title>Fallback</title><body>Direct page</body></html>"), nil
	})}

	result, err := Fetch(context.Background(), "https://example.com/page", FetchOptions{Timeout: time.Second, ToolEndpoint: "https://exa.test/contents", Client: client, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !exaHit || result.FetchMethod != "direct" || result.Title != "Fallback" || !strings.Contains(result.Text, "Direct page") {
		t.Fatalf("unexpected fallback result %#v exaHit=%v", result, exaHit)
	}
}

func TestFetchNonSuccessDirectFallsBackToToolEndpoint(t *testing.T) {
	var directHits, toolHits int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "exa.test" {
			toolHits++
			return testResponse(req, http.StatusOK, "application/json", `{"url":"https://example.com/not-found.txt","markdown":"Recovered"}`), nil
		}
		directHits++
		return testResponse(req, http.StatusNotFound, "text/plain", "not found"), nil
	})}

	result, err := Fetch(context.Background(), "https://example.com/not-found.txt", FetchOptions{Timeout: time.Second, ToolEndpoint: "https://exa.test/contents", Client: client, MaxBytes: 1024, MaxChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if directHits != 1 || toolHits != 1 || result.FetchMethod != "web_tool" || result.Text != "Recovered" {
		t.Fatalf("unexpected fallback result %#v directHits=%d toolHits=%d", result, directHits, toolHits)
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
		if payload["query"] != "query" || payload["limit"] != float64(5) {
			t.Fatalf("unexpected search payload %#v", payload)
		}
		_, _ = w.Write([]byte(`{"request_id":"req_1","search_context_size":"medium","results":[{"id":"doc_1","title":"One","url":"https://example.com","text":"ok","highlights":["hit"],"summary":"sum","published_at":"2026-01-01","site_name":"Example","author":"A","image_url":"https://example.com/image.png","favicon_url":"https://example.com/favicon.ico","metadata":{"links":["https://example.com/link"]}}]}`))
	}))
	defer server.Close()

	result, err := Search(context.Background(), "query", 5, SearchRequestOptions{}, SearchOptions{Enabled: true, Endpoint: server.URL, APIKey: "key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Query != "query" || result.RequestID != "req_1" || result.SearchContextSize != "medium" {
		t.Fatalf("missing top-level search metadata: %#v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "costDollars") {
		t.Fatalf("provider cost metadata leaked into search JSON: %s", string(raw))
	}
	if len(result.Results) != 1 || result.Results[0].ID != "doc_1" || result.Results[0].Title != "One" || result.Results[0].Snippet != "hit" || result.Results[0].Text != "ok" {
		t.Fatalf("unexpected search result %#v", result)
	}
	if result.Results[0].Published != "2026-01-01" || result.Results[0].PublishedAt != "2026-01-01" || result.Results[0].SiteName != "Example" || result.Results[0].SiteNameSnake != "Example" || result.Results[0].Author != "A" {
		t.Fatalf("missing search metadata: %#v", result.Results[0])
	}
	if len(result.Results[0].Highlights) != 1 || result.Results[0].Summary != "sum" || result.Results[0].ImageURL == "" || result.Results[0].FaviconURL == "" {
		t.Fatalf("missing search content fields: %#v", result.Results[0])
	}
	if result.Results[0].Metadata["links"] == nil {
		t.Fatalf("missing search metadata fields: %#v", result.Results[0])
	}
}

func TestSearchIncludesErrorResponseMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"Web tool upstream request timed out"}}`))
	}))
	defer server.Close()

	_, err := Search(context.Background(), "query", 5, SearchRequestOptions{}, SearchOptions{Enabled: true, Endpoint: server.URL, APIKey: "key", Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "Web tool upstream request timed out") {
		t.Fatalf("expected upstream error message to propagate, got %v", err)
	}
}

func TestSearchMapsToolOptionsToPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["category"] != "news" || payload["search_context_size"] != "high" {
			t.Fatalf("missing scalar options %#v", payload)
		}
		if domains, ok := payload["allowed_domains"].([]any); !ok || len(domains) != 1 || domains[0] != "example.com" {
			t.Fatalf("missing allowed_domains %#v", payload)
		}
		freshness, ok := payload["freshness"].(map[string]any)
		if !ok || freshness["days"] != float64(7) || freshness["published_before"] != "2026-06-01T00:00:00Z" {
			t.Fatalf("missing freshness %#v", payload)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	tool := WebSearchTool(SearchOptions{Enabled: true, Endpoint: server.URL, Timeout: time.Second})
	_, err := tool.Execute(context.Background(), "call", map[string]any{
		"query":               "query",
		"limit":               3,
		"allowed_domains":     []any{"example.com"},
		"search_context_size": "high",
		"category":            "news",
		"freshness":           map[string]any{"days": float64(7), "published_before": "2026-06-01T00:00:00Z"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchResultsCanBeFetchedByURL(t *testing.T) {
	var fetchedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"results":[{"title":"One","url":"https://example.com/one","snippet":"first"}]}`))
		case "/fetch":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			fetchedURL, _ = payload["url"].(string)
			_, _ = w.Write([]byte(`{"url":"` + fetchedURL + `","final_url":"` + fetchedURL + `","title":"One","markdown":"Full page"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "example.com" {
			return testResponse(req, http.StatusOK, "text/html", "<html><title>One</title><body>Search result page</body></html>"), nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})}

	tools := Tools(
		SessionInfo{},
		FetchOptions{Timeout: time.Second, ToolEndpoint: server.URL + "/fetch", Client: client},
		SearchOptions{Enabled: true, Endpoint: server.URL + "/search", Timeout: time.Second},
	)
	searchIndex := -1
	fetchIndex := -1
	for i := range tools {
		switch tools[i].Tool.Name {
		case "web_search":
			searchIndex = i
		case "fetch":
			fetchIndex = i
		}
	}
	if searchIndex < 0 || fetchIndex < 0 {
		t.Fatalf("missing tools")
	}
	properties, _ := tools[fetchIndex].Tool.Parameters["properties"].(map[string]any)
	if _, ok := properties["ref_id"]; ok {
		t.Fatalf("fetch schema should not expose ref_id, got %#v", tools[fetchIndex].Tool.Parameters)
	}
	if _, ok := properties["url"]; !ok {
		t.Fatalf("fetch schema should expose url, got %#v", tools[fetchIndex].Tool.Parameters)
	}
	required, _ := tools[fetchIndex].Tool.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "url" {
		t.Fatalf("fetch schema should require url, got %#v", tools[fetchIndex].Tool.Parameters)
	}

	searchResult, err := tools[searchIndex].Execute(context.Background(), "search-call", map[string]any{"query": "query"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var searchBody SearchResult
	if err := json.Unmarshal([]byte(searchResult.Content[0].Text), &searchBody); err != nil {
		t.Fatal(err)
	}
	if len(searchBody.Results) != 1 || searchBody.Results[0].URL != "https://example.com/one" {
		t.Fatalf("missing URL in search result: %#v", searchBody)
	}

	fetchResult, err := tools[fetchIndex].Execute(context.Background(), "fetch-call", map[string]any{"url": searchBody.Results[0].URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fetchBody FetchResult
	if err := json.Unmarshal([]byte(fetchResult.Content[0].Text), &fetchBody); err != nil {
		t.Fatal(err)
	}
	if fetchedURL != "https://example.com/one" || fetchBody.Markdown != "Full page" {
		t.Fatalf("unexpected fetch by URL: fetchedURL=%q result=%#v", fetchedURL, fetchBody)
	}

	fetchResult, err = tools[fetchIndex].Execute(context.Background(), "fetch-call-url", map[string]any{"url": "https://example.com/two"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fetchBody = FetchResult{}
	if err := json.Unmarshal([]byte(fetchResult.Content[0].Text), &fetchBody); err != nil {
		t.Fatal(err)
	}
	if fetchedURL != "https://example.com/two" || fetchBody.Markdown != "Full page" {
		t.Fatalf("unexpected fetch by URL target: fetchedURL=%q result=%#v", fetchedURL, fetchBody)
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
