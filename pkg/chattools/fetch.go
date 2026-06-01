package chattools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/net/html"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func FetchTool(options FetchOptions) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "fetch",
			Description: "Fetch a URL and return readable page content.",
			Parameters: objectSchema(map[string]any{
				"url":       map[string]any{"type": "string", "description": "The full HTTP/HTTPS URL to fetch."},
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
	directResult, directErr := fetchDirect(ctx, rawURL, parsed, options)
	if directErr == nil {
		if shouldReturnDirectResult(parsed, directResult.ContentType) {
			return directResult, nil
		}
		if isHTMLContentType(directResult.ContentType) || looksLikeHTML(directResult.RawBody) {
			if alternateURL := findReadableAlternate(directResult.ResponseHeaders, directResult.RawBody, directResult.FinalURL); alternateURL != "" {
				if alternate, err := fetchDirectURL(ctx, alternateURL, options); err == nil && shouldReturnDirectResult(mustParseURL(alternate.FinalURL), alternate.ContentType) {
					return alternate, nil
				}
			}
		}
	}
	if options.ToolEndpoint != "" {
		result, err := FetchContents(ctx, parsed.String(), options)
		if err == nil {
			return result, nil
		}
		zerolog.Ctx(ctx).Warn().
			Err(err).
			Str("action", "ai_tool_http").
			Str("tool", "fetch").
			Str("fetch_method", "web_tool").
			Str("target_url", parsed.Redacted()).
			Str("target_host", parsed.Host).
			Msg("Falling back to direct fetch result after web tool fetch failed")
	}
	if directErr != nil {
		return FetchResult{}, directErr
	}
	return directResult, nil
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
	req.Header.Set("Accept", directOpenAcceptHeader)
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
	markdown := ""
	if isDirectReadableContentType(resp.Header.Get("Content-Type")) || shouldReturnDirectURL(resp.Request.URL) {
		markdown = text
	}
	return FetchResult{
		URL:             rawURL,
		FinalURL:        resp.Request.URL.String(),
		Status:          resp.StatusCode,
		ContentType:     resp.Header.Get("Content-Type"),
		Title:           metadata.Title,
		Description:     metadata.Description,
		Text:            text,
		Markdown:        markdown,
		Favicon:         metadata.Favicon,
		Truncated:       truncated,
		FetchMethod:     "direct",
		ResponseHeaders: resp.Header.Clone(),
		RawBody:         body,
	}, nil
}

func fetchDirectURL(ctx context.Context, rawURL string, options FetchOptions) (FetchResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return FetchResult{}, fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return FetchResult{}, fmt.Errorf("unsupported URL scheme %s", parsed.Scheme)
	}
	return fetchDirect(ctx, rawURL, parsed, options)
}

func FetchContents(ctx context.Context, rawURL string, options FetchOptions) (FetchResult, error) {
	if options.ToolEndpoint == "" {
		return FetchResult{}, errors.New("fetch contents is not configured")
	}
	textMaxChars := options.MaxChars
	if textMaxChars <= 0 || textMaxChars > 50000 {
		textMaxChars = 20000
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	target, err := url.Parse(rawURL)
	if err != nil || target == nil || target.Scheme == "" || target.Host == "" {
		return FetchResult{}, fmt.Errorf("invalid URL")
	}
	log := toolHTTPLog(ctx, "fetch", http.MethodPost, options.ToolEndpoint).
		With().
		Str("fetch_method", "web_tool").
		Str("target_url", target.Redacted()).
		Str("target_host", target.Host).
		Logger()
	ctx = log.WithContext(ctx)
	payload, _ := json.Marshal(map[string]any{
		"url":       rawURL,
		"max_chars": textMaxChars,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, options.ToolEndpoint, bytes.NewReader(payload))
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
		URL:            firstNonEmpty(body.URL, rawURL),
		FinalURL:       firstNonEmpty(body.FinalURL, body.URL, rawURL),
		Status:         200,
		Title:          body.Title,
		Description:    body.Description,
		SiteName:       body.SiteName,
		Text:           firstNonEmpty(body.Markdown, body.Text),
		Markdown:       firstNonEmpty(body.Markdown, body.Text),
		Truncated:      body.Truncated,
		RequestID:      firstNonEmpty(body.RequestID, body.RequestIDSnake),
		RequestIDSnake: firstNonEmpty(body.RequestIDSnake, body.RequestID),
		Published:      firstNonEmpty(body.Published, body.PublishedAt, body.PublishedDate),
		Author:         body.Author,
		Image:          firstNonEmpty(body.Image, body.ImageURL),
		ImageURL:       firstNonEmpty(body.ImageURL, body.Image),
		Favicon:        firstNonEmpty(body.Favicon, body.FaviconURL),
		FaviconURL:     firstNonEmpty(body.FaviconURL, body.Favicon),
		Extras:         body.Metadata,
		FetchMethod:    "web_tool",
	}
	if len([]rune(result.Text)) > textMaxChars {
		runes := []rune(result.Text)
		result.Text = string(runes[:textMaxChars])
		result.Markdown = result.Text
		result.Truncated = true
	}
	log.Debug().
		Str("request_id", result.RequestID).
		Bool("truncated", result.Truncated).
		Msg("Parsed AI tool fetch result")
	return result, nil
}

const directOpenAcceptHeader = "text/markdown, text/plain;q=0.95, application/json;q=0.9, application/xml;q=0.85, text/xml;q=0.85, text/csv;q=0.85, text/html;q=0.5, */*;q=0.1"

func shouldReturnDirectResult(parsed *url.URL, contentType string) bool {
	return isDirectReadableContentType(contentType) || shouldReturnDirectURL(parsed)
}

func shouldReturnDirectURL(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
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
		".c", ".cc", ".cpp", ".h", ".hpp", ".java", ".kt", ".kts", ".rs", ".py", ".rb", ".swift", ".sh", ".bash", ".zsh", ".fish", ".sql":
		return true
	default:
		return false
	}
}

func isDirectReadableContentType(contentType string) bool {
	mediaType := normalizedMediaType(contentType)
	if mediaType == "" {
		return false
	}
	if mediaType == "text/markdown" || mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/xml" {
		return true
	}
	if strings.HasPrefix(mediaType, "text/") && mediaType != "text/html" {
		return true
	}
	if mediaType == "application/json" || mediaType == "application/xml" {
		return true
	}
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

func isHTMLContentType(contentType string) bool {
	mediaType := normalizedMediaType(contentType)
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

func looksLikeHTML(body []byte) bool {
	prefix := strings.ToLower(string(bytes.TrimSpace(body)))
	return strings.HasPrefix(prefix, "<!doctype html") || strings.HasPrefix(prefix, "<html")
}

func isReadableAlternateType(contentType string) bool {
	mediaType := normalizedMediaType(contentType)
	return mediaType == "text/markdown" || mediaType == "text/plain"
}

func normalizedMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return strings.ToLower(mediaType)
}

func findReadableAlternate(headers http.Header, body []byte, finalURL string) string {
	baseURL := mustParseURL(finalURL)
	if alt := readableAlternateFromLinkHeaders(headers.Values("Link"), baseURL); alt != "" {
		return alt
	}
	return readableAlternateFromHTML(body, baseURL)
}

func readableAlternateFromLinkHeaders(values []string, baseURL *url.URL) string {
	for _, value := range values {
		for _, part := range splitLinkHeader(value) {
			linkURL, params := parseLinkValue(part)
			if linkURL == "" {
				continue
			}
			if !relContains(params["rel"], "alternate") || !isReadableAlternateType(params["type"]) {
				continue
			}
			if resolved := resolveMetadataURL(linkURL, baseURL); resolved != "" {
				return resolved
			}
		}
	}
	return ""
}

func splitLinkHeader(value string) []string {
	var parts []string
	start := 0
	inQuote := false
	inAngle := false
	for i, r := range value {
		switch r {
		case '"':
			inQuote = !inQuote
		case '<':
			if !inQuote {
				inAngle = true
			}
		case '>':
			if !inQuote {
				inAngle = false
			}
		case ',':
			if !inQuote && !inAngle {
				parts = append(parts, strings.TrimSpace(value[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func parseLinkValue(value string) (string, map[string]string) {
	params := map[string]string{}
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "<") {
		return "", params
	}
	end := strings.Index(value, ">")
	if end < 0 {
		return "", params
	}
	linkURL := strings.TrimSpace(value[1:end])
	for _, part := range strings.Split(value[end+1:], ";") {
		key, raw, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(raw), `"`)
	}
	return linkURL, params
}

func readableAlternateFromHTML(body []byte, baseURL *url.URL) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var out string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if out != "" {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "link") {
			if relContains(attr(node, "rel"), "alternate") && isReadableAlternateType(attr(node, "type")) {
				out = resolveMetadataURL(attr(node, "href"), baseURL)
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return out
}

func relContains(rel string, token string) bool {
	for _, part := range strings.Fields(strings.ToLower(rel)) {
		if part == token {
			return true
		}
	}
	return false
}

func mustParseURL(rawURL string) *url.URL {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return parsed
}

type contentsResponse struct {
	URL            string         `json:"url"`
	FinalURL       string         `json:"final_url"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	SiteName       string         `json:"site_name"`
	Published      string         `json:"published"`
	PublishedAt    string         `json:"published_at"`
	PublishedDate  string         `json:"publishedDate"`
	Author         string         `json:"author"`
	Image          string         `json:"image"`
	ImageURL       string         `json:"image_url"`
	Favicon        string         `json:"favicon"`
	FaviconURL     string         `json:"favicon_url"`
	Text           string         `json:"text"`
	Markdown       string         `json:"markdown"`
	Truncated      bool           `json:"truncated"`
	Metadata       map[string]any `json:"metadata"`
	RequestID      string         `json:"requestId"`
	RequestIDSnake string         `json:"request_id"`
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
