package chattools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func WebSearchTool(options SearchOptions) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "web_search",
			Description: "Search the web and return relevant results with title, URL, snippets, readable content, and source metadata.",
			Parameters: objectSchema(map[string]any{
				"query":               map[string]any{"type": "string", "description": "Search query."},
				"limit":               map[string]any{"type": "integer", "description": "Maximum number of results, up to 10."},
				"search_context_size": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}, "description": "Amount of page context to include in each result."},
				"category":            map[string]any{"type": "string", "enum": []string{"web", "news", "research", "company", "financial_report", "people"}, "description": "Optional result category."},
				"allowed_domains":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Domains to include."},
				"freshness": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"days":             map[string]any{"type": "integer", "description": "Only include pages published in the last N days."},
						"published_after":  map[string]any{"type": "string", "description": "Only include pages published after this ISO 8601 timestamp."},
						"published_before": map[string]any{"type": "string", "description": "Only include pages published before this ISO 8601 timestamp."},
					},
				},
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
	log := toolHTTPLog(ctx, "web_search", http.MethodPost, options.Endpoint).
		With().
		Int("query_length", len([]rune(query))).
		Int("limit", limit).
		Logger()
	ctx = log.WithContext(ctx)
	payload, _ := json.Marshal(searchPayload(query, limit, request))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, options.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return SearchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if options.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+options.APIKey)
	}
	log.Trace().Msg("Sending AI tool HTTP request")
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Err(err).Dur("duration", time.Since(started)).Msg("AI tool HTTP request failed")
		return SearchResult{}, err
	}
	defer resp.Body.Close()
	logToolHTTPResponse(log, resp, time.Since(started), "Received AI tool HTTP response")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if message := errorMessageFromBody(body); message != "" {
			return SearchResult{}, fmt.Errorf("search failed with HTTP %d: %s", resp.StatusCode, message)
		}
		return SearchResult{}, fmt.Errorf("search failed with HTTP %d", resp.StatusCode)
	}
	var body searchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&body); err != nil {
		log.Err(err).Msg("Failed to parse AI tool HTTP response")
		return SearchResult{}, err
	}
	result := body.result()
	if result.Query == "" {
		result.Query = query
	}
	if len(result.Results) > limit {
		result.Results = result.Results[:limit]
	}
	log.Debug().
		Str("request_id", result.RequestID).
		Str("search_context_size", result.SearchContextSize).
		Int("result_count", len(result.Results)).
		Msg("Parsed AI tool search result")
	return result, nil
}

func errorMessageFromBody(body []byte) string {
	data := map[string]any{}
	if err := json.Unmarshal(body, &data); err != nil {
		return strings.TrimSpace(string(body))
	}
	if value := stringFromAnyValue(data["error"]); value != "" {
		return value
	}
	if errorData, _ := data["error"].(map[string]any); errorData != nil {
		if value := stringFromAnyValue(errorData["message"]); value != "" {
			return value
		}
	}
	if value := stringFromAnyValue(data["message"]); value != "" {
		return value
	}
	return ""
}

func stringFromAnyValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

type searchResponse struct {
	Query              string               `json:"query"`
	RequestID          string               `json:"requestId"`
	RequestIDSnake     string               `json:"request_id"`
	ResolvedSearchType string               `json:"resolvedSearchType"`
	SearchType         string               `json:"searchType"`
	SearchContextSize  string               `json:"search_context_size"`
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
	PublishedAt     string          `json:"published_at"`
	PublishedDate   string          `json:"publishedDate"`
	SiteName        string          `json:"siteName"`
	SiteNameSnake   string          `json:"site_name"`
	Author          string          `json:"author"`
	Image           string          `json:"image"`
	ImageURL        string          `json:"image_url"`
	Favicon         string          `json:"favicon"`
	FaviconURL      string          `json:"favicon_url"`
	Source          string          `json:"source"`
	Subpages        []SearchSubpage `json:"subpages"`
	Entities        []any           `json:"entities"`
	Extras          map[string]any  `json:"extras"`
	Metadata        map[string]any  `json:"metadata"`
}

func (body searchResponse) result() SearchResult {
	result := SearchResult{
		Query:              body.Query,
		RequestID:          firstNonEmpty(body.RequestID, body.RequestIDSnake),
		RequestIDSnake:     firstNonEmpty(body.RequestIDSnake, body.RequestID),
		ResolvedSearchType: body.ResolvedSearchType,
		SearchType:         body.SearchType,
		SearchContextSize:  body.SearchContextSize,
		Context:            body.Context,
		Output:             body.Output,
		Results:            make([]SearchItem, 0, len(body.Results)),
	}
	for _, item := range body.Results {
		snippet := firstNonEmpty(item.Snippet, firstString(item.Highlights), item.Summary, item.Text)
		published := firstNonEmpty(item.Published, item.PublishedAt, item.PublishedDate)
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
			PublishedAt:     firstNonEmpty(item.PublishedAt, published),
			PublishedDate:   item.PublishedDate,
			SiteName:        firstNonEmpty(item.SiteName, item.SiteNameSnake),
			SiteNameSnake:   firstNonEmpty(item.SiteNameSnake, item.SiteName),
			Author:          item.Author,
			Image:           firstNonEmpty(item.Image, item.ImageURL),
			ImageURL:        firstNonEmpty(item.ImageURL, item.Image),
			Favicon:         firstNonEmpty(item.Favicon, item.FaviconURL),
			FaviconURL:      firstNonEmpty(item.FaviconURL, item.Favicon),
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
		"query": query,
		"limit": limit,
	}
	addString(payload, "search_context_size", request.SearchContextSize)
	addString(payload, "category", request.Category)
	addStrings(payload, "allowed_domains", request.AllowedDomains)
	if request.Freshness != nil {
		freshness := map[string]any{}
		if request.Freshness.Days > 0 {
			freshness["days"] = request.Freshness.Days
		}
		addString(freshness, "published_after", request.Freshness.PublishedAfter)
		addString(freshness, "published_before", request.Freshness.PublishedBefore)
		if len(freshness) > 0 {
			payload["freshness"] = freshness
		}
	}
	return payload
}

func requestOptions(params any) SearchRequestOptions {
	values, ok := params.(map[string]any)
	if !ok {
		return SearchRequestOptions{}
	}
	var out SearchRequestOptions
	out.SearchContextSize = stringValueParam(values, "search_context_size")
	out.Category = stringValueParam(values, "category")
	out.AllowedDomains = stringSliceParam(values, "allowed_domains")
	if freshness := mapParam(values, "freshness"); freshness != nil {
		out.Freshness = &SearchFreshness{
			Days:            intParam(freshness, "days", 0),
			PublishedAfter:  stringValueParam(freshness, "published_after"),
			PublishedBefore: stringValueParam(freshness, "published_before"),
		}
	}
	return out
}
