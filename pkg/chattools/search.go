package chattools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

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
		Str("resolved_search_type", result.ResolvedSearchType).
		Int("result_count", len(result.Results)).
		Msg("Parsed AI tool search result")
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

func (body searchResponse) result() SearchResult {
	result := SearchResult{
		Query:              body.Query,
		RequestID:          body.RequestID,
		ResolvedSearchType: body.ResolvedSearchType,
		SearchType:         body.SearchType,
		Context:            body.Context,
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
