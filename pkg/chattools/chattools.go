package chattools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type SessionInfo struct {
	Timestamp       string         `json:"timestamp"`
	Timezone        string         `json:"timezone"`
	RoomTitle       string         `json:"room_title,omitempty"`
	RoomID          string         `json:"room_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	ThreadID        string         `json:"thread_id,omitempty"`
	LoginID         string         `json:"login_id,omitempty"`
	ProviderID      string         `json:"provider_id,omitempty"`
	ModelID         string         `json:"model_id,omitempty"`
	ReasoningLevel  string         `json:"reasoning_level,omitempty"`
	DisabledTools   []string       `json:"disabled_tools,omitempty"`
	AttachmentCount int            `json:"attachment_count"`
	Attachments     []Attachment   `json:"attachments,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

type Attachment struct {
	Type     string `json:"type,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

type FetchOptions struct {
	Timeout     time.Duration
	MaxBytes    int64
	MaxChars    int
	Client      *http.Client
	ExaEndpoint string
	APIKey      string
}

type SearchOptions struct {
	Enabled  bool
	Endpoint string
	APIKey   string
	Timeout  time.Duration
	Client   *http.Client
}

type SearchRequestOptions struct {
	IncludeDomains     []string       `json:"includeDomains,omitempty"`
	ExcludeDomains     []string       `json:"excludeDomains,omitempty"`
	StartCrawlDate     string         `json:"startCrawlDate,omitempty"`
	EndCrawlDate       string         `json:"endCrawlDate,omitempty"`
	StartPublishedDate string         `json:"startPublishedDate,omitempty"`
	EndPublishedDate   string         `json:"endPublishedDate,omitempty"`
	Context            any            `json:"context,omitempty"`
	Moderation         *bool          `json:"moderation,omitempty"`
	Contents           map[string]any `json:"contents,omitempty"`
	AdditionalQueries  []string       `json:"additionalQueries,omitempty"`
	Type               string         `json:"type,omitempty"`
	Category           string         `json:"category,omitempty"`
	UserLocation       string         `json:"userLocation,omitempty"`
	Compliance         string         `json:"compliance,omitempty"`
	OutputSchema       map[string]any `json:"outputSchema,omitempty"`
	SystemPrompt       string         `json:"systemPrompt,omitempty"`
}

func Tools(info SessionInfo, fetch FetchOptions, search SearchOptions) []agent.AgentTool[any] {
	tools := []agent.AgentTool[any]{
		GetSessionTool(info),
		FetchTool(fetch),
	}
	if search.Enabled {
		tools = append(tools, WebSearchTool(search))
	}
	return tools
}

func GetSessionTool(info SessionInfo) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "get_session",
			Description: "Get fresh metadata for this Beeper AI chat, including current timestamp, timezone, room, session, model, reasoning, search, and attachments.",
			Parameters:  objectSchema(nil, nil),
		},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
			now := time.Now()
			current := info
			current.Timestamp = now.Format(time.RFC3339)
			current.Timezone = now.Location().String()
			if current.ThreadID == "" {
				current.ThreadID = current.SessionID
			}
			return jsonResult(current)
		},
	}
}

func FetchTool(options FetchOptions) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "fetch",
			Description: "Fetch an HTTP or HTTPS URL. Uses direct HTTP fetch for assets, markdown, text, and data files; uses Exa page extraction for normal web pages when configured.",
			Parameters: objectSchema(map[string]any{
				"url":       map[string]any{"type": "string", "description": "HTTP or HTTPS URL to fetch."},
				"max_chars": map[string]any{"type": "integer", "description": "Maximum number of text characters to return."},
			}, []string{"url"}),
		},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
			urlValue, err := stringParam(params, "url")
			if err != nil {
				return agent.AgentToolResult[any]{}, err
			}
			fetchOptions := options
			if maxChars := intParam(params, "max_chars", 0); maxChars > 0 {
				fetchOptions.MaxChars = maxChars
			}
			result, err := Fetch(ctx, urlValue, fetchOptions)
			if err != nil {
				return agent.AgentToolResult[any]{}, err
			}
			return jsonResult(result)
		},
	}
}

type FetchResult struct {
	URL             string          `json:"url"`
	FinalURL        string          `json:"final_url"`
	Status          int             `json:"status"`
	ContentType     string          `json:"content_type,omitempty"`
	Title           string          `json:"title,omitempty"`
	Text            string          `json:"text,omitempty"`
	Truncated       bool            `json:"truncated"`
	ID              string          `json:"id,omitempty"`
	Published       string          `json:"published,omitempty"`
	Author          string          `json:"author,omitempty"`
	Image           string          `json:"image,omitempty"`
	Favicon         string          `json:"favicon,omitempty"`
	Highlights      []string        `json:"highlights,omitempty"`
	HighlightScores []float64       `json:"highlightScores,omitempty"`
	Summary         any             `json:"summary,omitempty"`
	Subpages        []SearchSubpage `json:"subpages,omitempty"`
	Entities        []any           `json:"entities,omitempty"`
	Extras          map[string]any  `json:"extras,omitempty"`
	Source          string          `json:"source,omitempty"`
	RequestID       string          `json:"requestId,omitempty"`
	Context         string          `json:"context,omitempty"`
	CostDollars     map[string]any  `json:"costDollars,omitempty"`
	Error           string          `json:"error,omitempty"`
	FetchMethod     string          `json:"fetch_method,omitempty"`
}

func Fetch(ctx context.Context, rawURL string, options FetchOptions) (FetchResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return FetchResult{}, fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return FetchResult{}, fmt.Errorf("unsupported URL scheme %s", parsed.Scheme)
	}
	if options.Timeout == 0 {
		options.Timeout = 10 * time.Second
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = 2 * 1024 * 1024
	}
	if options.MaxChars == 0 {
		options.MaxChars = 20000
	}
	if options.ExaEndpoint != "" && !shouldDirectFetch(parsed) {
		result, err := FetchContents(ctx, parsed.String(), options)
		if err == nil {
			return result, nil
		}
	}
	return fetchDirect(ctx, rawURL, parsed, options)
}

func fetchDirect(ctx context.Context, rawURL string, parsed *url.URL, options FetchOptions) (FetchResult, error) {
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("User-Agent", "beeper-ai-bridge/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, options.MaxBytes+1))
	if err != nil {
		return FetchResult{}, err
	}
	truncated := int64(len(body)) > options.MaxBytes
	if truncated {
		body = body[:options.MaxBytes]
	}
	text := extractText(body, resp.Header.Get("Content-Type"))
	if len([]rune(text)) > options.MaxChars {
		runes := []rune(text)
		text = string(runes[:options.MaxChars])
		truncated = true
	}
	return FetchResult{
		URL:         rawURL,
		FinalURL:    resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Title:       extractTitle(body),
		Text:        text,
		Truncated:   truncated,
		FetchMethod: "direct",
	}, nil
}

func FetchContents(ctx context.Context, rawURL string, options FetchOptions) (FetchResult, error) {
	if options.ExaEndpoint == "" {
		return FetchResult{}, errors.New("fetch contents is not configured")
	}
	textMaxChars := options.MaxChars
	if textMaxChars <= 0 || textMaxChars > 10000 {
		textMaxChars = 10000
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	payload, _ := json.Marshal(map[string]any{
		"urls": []string{rawURL},
		"text": map[string]any{
			"maxCharacters": textMaxChars,
			"verbosity":     "standard",
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, options.ExaEndpoint, bytes.NewReader(payload))
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if options.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+options.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchResult{}, fmt.Errorf("fetch contents failed with HTTP %d", resp.StatusCode)
	}
	var body contentsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&body); err != nil {
		return FetchResult{}, err
	}
	result := FetchResult{
		URL:         rawURL,
		FinalURL:    rawURL,
		Status:      200,
		Truncated:   false,
		RequestID:   body.RequestID,
		Context:     body.Context,
		CostDollars: body.CostDollars,
		FetchMethod: "exa",
	}
	if len(body.Statuses) > 0 {
		status := body.Statuses[0]
		result.Source = status.Source
		if status.Status == "error" {
			result.Status = status.Error.HTTPStatusCode
			if result.Status == 0 {
				result.Status = 502
			}
			result.Error = status.Error.Tag
			return result, fmt.Errorf("fetch contents failed: %s", firstNonEmpty(status.Error.Tag, status.Status))
		}
	}
	if len(body.Results) == 0 {
		return result, nil
	}
	item := body.Results[0]
	result.FinalURL = firstNonEmpty(item.URL, rawURL)
	result.ID = item.ID
	result.Title = item.Title
	result.Text = item.Text
	result.Published = firstNonEmpty(item.Published, item.PublishedDate)
	result.Author = item.Author
	result.Image = item.Image
	result.Favicon = item.Favicon
	result.Highlights = item.Highlights
	result.HighlightScores = item.HighlightScores
	result.Summary = item.Summary
	result.Subpages = item.Subpages
	result.Entities = item.Entities
	result.Extras = item.Extras
	if len([]rune(result.Text)) > options.MaxChars {
		runes := []rune(result.Text)
		result.Text = string(runes[:options.MaxChars])
		result.Truncated = true
	}
	return result, nil
}

func WebSearchTool(options SearchOptions) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "web_search",
			Description: "Search the web with Exa and return relevant results with title, URL, content, highlights, summaries, and source metadata.",
			Parameters: objectSchema(map[string]any{
				"query":              map[string]any{"type": "string", "description": "Search query."},
				"limit":              map[string]any{"type": "integer", "description": "Maximum number of results. Sent to Exa as numResults."},
				"includeDomains":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Domains or domain paths to include."},
				"excludeDomains":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Domains or domain paths to exclude."},
				"startCrawlDate":     map[string]any{"type": "string", "description": "Only include results crawled after this ISO 8601 timestamp."},
				"endCrawlDate":       map[string]any{"type": "string", "description": "Only include results crawled before this ISO 8601 timestamp."},
				"startPublishedDate": map[string]any{"type": "string", "description": "Only include results published after this ISO 8601 timestamp."},
				"endPublishedDate":   map[string]any{"type": "string", "description": "Only include results published before this ISO 8601 timestamp."},
				"context":            map[string]any{"description": "Deprecated Exa context option. Prefer contents.text, contents.highlights, or contents.summary."},
				"moderation":         map[string]any{"type": "boolean", "description": "Enable Exa content moderation."},
				"contents":           map[string]any{"type": "object", "description": "Exa contents options for text, highlights, summary, extras, freshness, and subpages."},
				"additionalQueries":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Additional deep-search query variations."},
				"type":               map[string]any{"type": "string", "enum": []string{"instant", "fast", "auto", "deep-lite", "deep", "deep-reasoning"}, "description": "Exa search type."},
				"category":           map[string]any{"type": "string", "enum": []string{"company", "research paper", "news", "personal site", "financial report", "people"}, "description": "Exa search category."},
				"userLocation":       map[string]any{"type": "string", "description": "Two-letter country code used to bias results."},
				"compliance":         map[string]any{"type": "string", "enum": []string{"hipaa"}, "description": "Enterprise-only compliance mode."},
				"outputSchema":       map[string]any{"type": "object", "description": "Exa synthesis output schema. Do not combine with Exa streaming."},
				"systemPrompt":       map[string]any{"type": "string", "description": "Additional Exa synthesis/search instructions."},
			}, []string{"query"}),
		},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
			query, err := stringParam(params, "query")
			if err != nil {
				return agent.AgentToolResult[any]{}, err
			}
			limit := intParam(params, "limit", 5)
			result, err := Search(ctx, query, limit, requestOptions(params), options)
			if err != nil {
				return agent.AgentToolResult[any]{}, err
			}
			return jsonResult(result)
		},
	}
}

type SearchResult struct {
	Query              string         `json:"query"`
	RequestID          string         `json:"requestId,omitempty"`
	ResolvedSearchType string         `json:"resolvedSearchType,omitempty"`
	SearchType         string         `json:"searchType,omitempty"`
	Context            string         `json:"context,omitempty"`
	CostDollars        map[string]any `json:"costDollars,omitempty"`
	Output             map[string]any `json:"output,omitempty"`
	Results            []SearchItem   `json:"results"`
}

type SearchItem struct {
	ID              string          `json:"id,omitempty"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	Snippet         string          `json:"snippet,omitempty"`
	Text            string          `json:"text,omitempty"`
	Highlights      []string        `json:"highlights,omitempty"`
	HighlightScores []float64       `json:"highlightScores,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	Description     string          `json:"description,omitempty"`
	Published       string          `json:"published,omitempty"`
	PublishedDate   string          `json:"publishedDate,omitempty"`
	SiteName        string          `json:"siteName,omitempty"`
	Author          string          `json:"author,omitempty"`
	Image           string          `json:"image,omitempty"`
	Favicon         string          `json:"favicon,omitempty"`
	Source          string          `json:"source,omitempty"`
	Subpages        []SearchSubpage `json:"subpages,omitempty"`
	Entities        []any           `json:"entities,omitempty"`
	Extras          map[string]any  `json:"extras,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

type SearchSubpage struct {
	ID            string `json:"id,omitempty"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	PublishedDate string `json:"publishedDate,omitempty"`
	Published     string `json:"published,omitempty"`
	Author        string `json:"author,omitempty"`
	Image         string `json:"image,omitempty"`
	Favicon       string `json:"favicon,omitempty"`
}

func Search(ctx context.Context, query string, limit int, request SearchRequestOptions, options SearchOptions) (SearchResult, error) {
	if !options.Enabled || options.Endpoint == "" {
		return SearchResult{}, errors.New("web_search is not configured")
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	if options.Timeout == 0 {
		options.Timeout = 10 * time.Second
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	payload, _ := json.Marshal(searchPayload(query, limit, request))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, options.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return SearchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if options.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+options.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return SearchResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("search failed with HTTP %d", resp.StatusCode)
	}
	var body searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&body); err != nil {
		return SearchResult{}, err
	}
	result := body.result()
	if result.Query == "" {
		result.Query = query
	}
	if len(result.Results) > limit {
		result.Results = result.Results[:limit]
	}
	return result, nil
}

type searchResponse struct {
	Query              string               `json:"query"`
	RequestID          string               `json:"requestId"`
	ResolvedSearchType string               `json:"resolvedSearchType"`
	SearchType         string               `json:"searchType"`
	Context            string               `json:"context"`
	CostDollars        map[string]any       `json:"costDollars"`
	Output             map[string]any       `json:"output"`
	Results            []searchResponseItem `json:"results"`
}

type searchResponseItem struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	Snippet         string          `json:"snippet"`
	Text            string          `json:"text"`
	Highlights      []string        `json:"highlights"`
	HighlightScores []float64       `json:"highlightScores"`
	Summary         string          `json:"summary"`
	Description     string          `json:"description"`
	Published       string          `json:"published"`
	PublishedDate   string          `json:"publishedDate"`
	SiteName        string          `json:"siteName"`
	Author          string          `json:"author"`
	Image           string          `json:"image"`
	Favicon         string          `json:"favicon"`
	Source          string          `json:"source"`
	Subpages        []SearchSubpage `json:"subpages"`
	Entities        []any           `json:"entities"`
	Extras          map[string]any  `json:"extras"`
	Metadata        map[string]any  `json:"metadata"`
}

type contentsResponse struct {
	RequestID   string                `json:"requestId"`
	Context     string                `json:"context"`
	CostDollars map[string]any        `json:"costDollars"`
	Results     []contentsResultItem  `json:"results"`
	Statuses    []contentsStatusEntry `json:"statuses"`
}

type contentsResultItem struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	Text            string          `json:"text"`
	Highlights      []string        `json:"highlights"`
	HighlightScores []float64       `json:"highlightScores"`
	Summary         any             `json:"summary"`
	Published       string          `json:"published"`
	PublishedDate   string          `json:"publishedDate"`
	Author          string          `json:"author"`
	Image           string          `json:"image"`
	Favicon         string          `json:"favicon"`
	Subpages        []SearchSubpage `json:"subpages"`
	Entities        []any           `json:"entities"`
	Extras          map[string]any  `json:"extras"`
}

type contentsStatusEntry struct {
	ID     string              `json:"id"`
	Status string              `json:"status"`
	Source string              `json:"source"`
	Error  contentsStatusError `json:"error"`
}

type contentsStatusError struct {
	Tag            string `json:"tag"`
	HTTPStatusCode int    `json:"httpStatusCode"`
}

func (body searchResponse) result() SearchResult {
	result := SearchResult{
		Query:              body.Query,
		RequestID:          body.RequestID,
		ResolvedSearchType: body.ResolvedSearchType,
		SearchType:         body.SearchType,
		Context:            body.Context,
		CostDollars:        body.CostDollars,
		Output:             body.Output,
		Results:            make([]SearchItem, 0, len(body.Results)),
	}
	for _, item := range body.Results {
		snippet := firstNonEmpty(item.Snippet, firstString(item.Highlights), item.Summary, item.Text)
		published := item.Published
		if published == "" {
			published = item.PublishedDate
		}
		result.Results = append(result.Results, SearchItem{
			ID:              item.ID,
			Title:           item.Title,
			URL:             item.URL,
			Snippet:         snippet,
			Text:            item.Text,
			Highlights:      item.Highlights,
			HighlightScores: item.HighlightScores,
			Summary:         item.Summary,
			Description:     item.Description,
			Published:       published,
			PublishedDate:   item.PublishedDate,
			SiteName:        item.SiteName,
			Author:          item.Author,
			Image:           item.Image,
			Favicon:         item.Favicon,
			Source:          item.Source,
			Subpages:        item.Subpages,
			Entities:        item.Entities,
			Extras:          item.Extras,
			Metadata:        item.Metadata,
		})
	}
	return result
}

func searchPayload(query string, limit int, request SearchRequestOptions) map[string]any {
	payload := map[string]any{
		"query":      query,
		"numResults": limit,
	}
	addStrings(payload, "includeDomains", request.IncludeDomains)
	addStrings(payload, "excludeDomains", request.ExcludeDomains)
	addString(payload, "startCrawlDate", request.StartCrawlDate)
	addString(payload, "endCrawlDate", request.EndCrawlDate)
	addString(payload, "startPublishedDate", request.StartPublishedDate)
	addString(payload, "endPublishedDate", request.EndPublishedDate)
	addAny(payload, "context", request.Context)
	if request.Moderation != nil {
		payload["moderation"] = *request.Moderation
	}
	addMap(payload, "contents", request.Contents)
	addStrings(payload, "additionalQueries", request.AdditionalQueries)
	addString(payload, "type", request.Type)
	addString(payload, "category", request.Category)
	addString(payload, "userLocation", request.UserLocation)
	addString(payload, "compliance", request.Compliance)
	addMap(payload, "outputSchema", request.OutputSchema)
	addString(payload, "systemPrompt", request.SystemPrompt)
	return payload
}

func requestOptions(params any) SearchRequestOptions {
	values, ok := params.(map[string]any)
	if !ok {
		return SearchRequestOptions{}
	}
	var out SearchRequestOptions
	out.IncludeDomains = stringSliceParam(values, "includeDomains")
	out.ExcludeDomains = stringSliceParam(values, "excludeDomains")
	out.StartCrawlDate = stringValueParam(values, "startCrawlDate")
	out.EndCrawlDate = stringValueParam(values, "endCrawlDate")
	out.StartPublishedDate = stringValueParam(values, "startPublishedDate")
	out.EndPublishedDate = stringValueParam(values, "endPublishedDate")
	if value, ok := values["context"]; ok {
		out.Context = value
	}
	if value, ok := values["moderation"].(bool); ok {
		out.Moderation = &value
	}
	out.Contents = mapParam(values, "contents")
	out.AdditionalQueries = stringSliceParam(values, "additionalQueries")
	out.Type = stringValueParam(values, "type")
	out.Category = stringValueParam(values, "category")
	out.UserLocation = stringValueParam(values, "userLocation")
	out.Compliance = stringValueParam(values, "compliance")
	out.OutputSchema = mapParam(values, "outputSchema")
	out.SystemPrompt = stringValueParam(values, "systemPrompt")
	return out
}

func addString(payload map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		payload[key] = strings.TrimSpace(value)
	}
}

func addStrings(payload map[string]any, key string, values []string) {
	if len(values) > 0 {
		payload[key] = values
	}
}

func addMap(payload map[string]any, key string, value map[string]any) {
	if len(value) > 0 {
		payload[key] = value
	}
}

func addAny(payload map[string]any, key string, value any) {
	if value != nil {
		payload[key] = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func shouldDirectFetch(parsed *url.URL) bool {
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.EscapedPath())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return true
	}
	if host == "raw.githubusercontent.com" || host == "gist.githubusercontent.com" {
		return true
	}
	if strings.Contains(path, "/-/raw/") || strings.Contains(path, "/raw/") {
		return true
	}
	dot := strings.LastIndex(path, ".")
	if dot < 0 {
		return false
	}
	switch path[dot:] {
	case ".txt", ".md", ".markdown", ".rst", ".csv", ".tsv", ".json", ".jsonl", ".yaml", ".yml", ".toml", ".xml", ".rss", ".atom",
		".log", ".diff", ".patch",
		".go", ".js", ".jsx", ".ts", ".tsx", ".css", ".scss", ".sass", ".less",
		".c", ".cc", ".cpp", ".h", ".hpp", ".java", ".kt", ".kts", ".rs", ".py", ".rb", ".swift", ".sh", ".bash", ".zsh", ".fish", ".sql",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico", ".svg",
		".pdf", ".zip", ".tar", ".tgz", ".gz", ".bz2", ".xz", ".7z", ".rar",
		".mp3", ".mp4", ".m4a", ".mov", ".wav", ".webm", ".ogg",
		".woff", ".woff2", ".ttf", ".otf", ".eot", ".wasm",
		".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return true
	default:
		return false
	}
}

func jsonResult(value any) (agent.AgentToolResult[any], error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return agent.AgentToolResult[any]{}, err
	}
	return agent.AgentToolResult[any]{
		Content: []ai.ContentBlock{{Type: "text", Text: string(raw)}},
		Details: value,
	}, nil
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

func stringParam(params any, key string) (string, error) {
	values, ok := params.(map[string]any)
	if !ok {
		return "", fmt.Errorf("expected object arguments")
	}
	value, ok := values[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}

func intParam(params any, key string, fallback int) int {
	values, ok := params.(map[string]any)
	if !ok {
		return fallback
	}
	switch value := values[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func stringValueParam(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func stringSliceParam(values map[string]any, key string) []string {
	raw, ok := values[key].([]any)
	if !ok {
		if typed, ok := values[key].([]string); ok {
			return cleanStringSlice(typed)
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func mapParam(values map[string]any, key string) map[string]any {
	value, ok := values[key].(map[string]any)
	if !ok || len(value) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range value {
		out[k] = v
	}
	return out
}

var titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
var scriptStyleRE = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
var tagRE = regexp.MustCompile(`(?is)<[^>]+>`)
var whitespaceRE = regexp.MustCompile(`\s+`)

func extractTitle(body []byte) string {
	match := titleRE.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return cleanText(string(match[1]))
}

func extractText(body []byte, contentType string) string {
	raw := string(body)
	if strings.Contains(strings.ToLower(contentType), "html") || strings.Contains(raw, "<html") {
		raw = scriptStyleRE.ReplaceAllString(raw, " ")
		raw = tagRE.ReplaceAllString(raw, " ")
	}
	return cleanText(raw)
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = whitespaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}
