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
	"strings"
	"time"

	"github.com/rs/zerolog"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func FetchTool(options FetchOptions) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "fetch",
			Description: "Fetch an HTTP or HTTPS URL and return readable page content, metadata, and source details.",
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
		zerolog.Ctx(ctx).Warn().
			Err(err).
			Str("action", "ai_tool_http").
			Str("tool", "fetch").
			Str("fetch_method", "exa").
			Str("target_url", parsed.Redacted()).
			Str("target_host", parsed.Host).
			Msg("Falling back to direct fetch after Exa fetch failed")
	}
	return fetchDirect(ctx, rawURL, parsed, options)
}

func fetchDirect(ctx context.Context, rawURL string, parsed *url.URL, options FetchOptions) (FetchResult, error) {
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	log := toolHTTPLog(ctx, "fetch", http.MethodGet, parsed.String()).
		With().
		Str("fetch_method", "direct").
		Str("target_url", parsed.Redacted()).
		Str("target_host", parsed.Host).
		Logger()
	ctx = log.WithContext(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("User-Agent", "beeper-ai-bridge/1.0")
	log.Trace().Msg("Sending AI tool HTTP request")
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Err(err).Dur("duration", time.Since(started)).Msg("AI tool HTTP request failed")
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	logToolHTTPResponse(log, resp, time.Since(started), "Received AI tool HTTP response")
	body, err := io.ReadAll(io.LimitReader(resp.Body, options.MaxBytes+1))
	if err != nil {
		log.Err(err).Dur("duration", time.Since(started)).Msg("Failed to read AI tool HTTP response")
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
	metadata := extractHTMLMetadata(body, resp.Request.URL)
	return FetchResult{
		URL:         rawURL,
		FinalURL:    resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Title:       metadata.Title,
		Description: metadata.Description,
		Text:        text,
		Favicon:     metadata.Favicon,
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
	target, err := url.Parse(rawURL)
	if err != nil || target == nil || target.Scheme == "" || target.Host == "" {
		return FetchResult{}, fmt.Errorf("invalid URL")
	}
	log := toolHTTPLog(ctx, "fetch", http.MethodPost, options.ExaEndpoint).
		With().
		Str("fetch_method", "exa").
		Str("target_url", target.Redacted()).
		Str("target_host", target.Host).
		Logger()
	ctx = log.WithContext(ctx)
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
	log.Trace().Msg("Sending AI tool HTTP request")
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Err(err).Dur("duration", time.Since(started)).Msg("AI tool HTTP request failed")
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	logToolHTTPResponse(log, resp, time.Since(started), "Received AI tool HTTP response")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchResult{}, fmt.Errorf("fetch contents failed with HTTP %d", resp.StatusCode)
	}
	var body contentsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&body); err != nil {
		log.Err(err).Msg("Failed to parse AI tool HTTP response")
		return FetchResult{}, err
	}
	result := FetchResult{
		URL:         rawURL,
		FinalURL:    rawURL,
		Status:      200,
		Truncated:   false,
		RequestID:   body.RequestID,
		Context:     body.Context,
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
			log.Error().
				Int("status_code", result.Status).
				Str("request_id", body.RequestID).
				Str("error_tag", status.Error.Tag).
				Msg("AI tool fetch provider returned item error")
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
	result.Description = item.Description
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
	if len([]rune(result.Text)) > textMaxChars {
		runes := []rune(result.Text)
		result.Text = string(runes[:textMaxChars])
		result.Truncated = true
	}
	log.Debug().
		Str("request_id", body.RequestID).
		Bool("truncated", result.Truncated).
		Msg("Parsed AI tool fetch result")
	return result, nil
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
	Description     string          `json:"description"`
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

func toolHTTPLog(ctx context.Context, tool string, method string, rawURL string) zerolog.Logger {
	logCtx := zerolog.Ctx(ctx).With().
		Str("action", "ai_tool_http").
		Str("tool", tool).
		Str("method", method)
	if parsed, err := url.Parse(rawURL); err == nil {
		logCtx = logCtx.Str("url", parsed.Redacted()).Str("host", parsed.Host).Str("path", parsed.EscapedPath())
	} else {
		logCtx = logCtx.Str("url", rawURL)
	}
	return logCtx.Logger()
}

func logToolHTTPResponse(log zerolog.Logger, resp *http.Response, duration time.Duration, message string) {
	logEvent := log.Debug()
	if resp.StatusCode >= 400 {
		logEvent = log.Error()
	}
	logEvent.
		Dur("duration", duration).
		Int("status_code", resp.StatusCode).
		Str("status", resp.Status).
		Int64("response_content_length", resp.ContentLength).
		Str("response_content_type", resp.Header.Get("Content-Type")).
		Msg(message)
}
