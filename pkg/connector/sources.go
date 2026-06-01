package connector

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type sourceCollector struct {
	byID  map[string]*canonicalSource
	order []string
}

type sourceObservation struct {
	URL         string
	Title       string
	Description string
	SiteName    string
	FaviconURL  string
	ImageURL    string
	PublishedAt string
	Priority    int
	Appearance  sourceAppearance
}

type canonicalSource struct {
	SourceID    string
	URL         string
	Title       string
	Description string
	SiteName    string
	FaviconURL  string
	ImageURL    string
	PublishedAt string
	Appearances []sourceAppearance
	fieldScore  map[string]int
	seen        map[string]struct{}
}

type sourceAppearance struct {
	Kind         string
	ToolCallID   string
	ToolName     string
	Query        string
	Rank         int
	Cited        bool
	StartIndex   *int
	EndIndex     *int
	ContentIndex *int
	Text         string
}

func newSourceCollector() *sourceCollector {
	return &sourceCollector{byID: map[string]*canonicalSource{}}
}

func (c *sourceCollector) addToolOutput(output toolOutputEvent, result any) []map[string]any {
	if c == nil || output.IsError {
		return nil
	}
	switch output.Name {
	case "web_search":
		return c.addWebSearchOutput(output, result)
	case "fetch":
		return c.addFetchOutput(output, result)
	default:
		return nil
	}
}

func (c *sourceCollector) addWebSearchOutput(output toolOutputEvent, result any) []map[string]any {
	data := mapFromAny(result)
	if data == nil {
		return nil
	}
	rawResults, _ := data["results"].([]any)
	if len(rawResults) == 0 {
		return nil
	}
	input := mapFromAny(output.Input)
	query := firstSourceString(stringFromAny(data["query"]), stringFromAny(input["query"]))
	changed := make([]map[string]any, 0, len(rawResults))
	for index, raw := range rawResults {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if updated := c.addSearchResultSource(output, query, index+1, item, 50, false); updated != nil {
			changed = append(changed, updated)
		}
		for _, rawSubpage := range sourceSlice(item, "subpages") {
			subpage, ok := rawSubpage.(map[string]any)
			if !ok {
				continue
			}
			if updated := c.addSearchResultSource(output, query, index+1, subpage, 50, false); updated != nil {
				changed = append(changed, updated)
			}
		}
	}
	for _, updated := range c.addWebSearchGroundingSources(output, data, query) {
		changed = append(changed, updated)
	}
	return changed
}

func (c *sourceCollector) addSearchResultSource(output toolOutputEvent, query string, rank int, item map[string]any, priority int, cited bool) map[string]any {
	source := sourceObservation{
		URL:         sourceString(item, "url", "URL", "uri"),
		Title:       sourceString(item, "title"),
		Description: firstSourceString(sourceDescriptionString(item), firstStringFromSlice(item["highlights"])),
		SiteName:    sourceString(item, "siteName", "site_name", "source"),
		FaviconURL:  sourceFaviconString(item),
		ImageURL:    sourceImageString(item),
		PublishedAt: sourceString(item, "published", "publishedAt", "published_at", "publishedDate", "datePublished", "date"),
		Priority:    priority,
		Appearance: sourceAppearance{
			Kind:       "web_search",
			ToolCallID: output.ID,
			ToolName:   output.Name,
			Query:      query,
			Rank:       rank,
			Cited:      cited,
		},
	}
	if nested, ok := item["metadata"].(map[string]any); ok {
		source.Title = firstSourceString(source.Title, sourceString(nested, "title", "ogTitle", "openGraphTitle"))
		source.Description = firstSourceString(source.Description, sourceDescriptionString(nested))
		source.SiteName = firstSourceString(source.SiteName, sourceString(nested, "siteName", "site_name", "ogSiteName"))
		source.FaviconURL = firstSourceString(source.FaviconURL, sourceFaviconString(nested))
		source.ImageURL = firstSourceString(source.ImageURL, sourceImageString(nested))
		source.PublishedAt = firstSourceString(source.PublishedAt, sourceString(nested, "published", "publishedAt", "published_at", "publishedDate", "datePublished", "date"))
	}
	return c.add(source)
}

func (c *sourceCollector) addWebSearchGroundingSources(output toolOutputEvent, data map[string]any, query string) []map[string]any {
	outputData, ok := data["output"].(map[string]any)
	if !ok {
		return nil
	}
	grounding := sourceSlice(outputData, "grounding")
	changed := make([]map[string]any, 0, len(grounding))
	for _, rawGrounding := range grounding {
		item, ok := rawGrounding.(map[string]any)
		if !ok {
			continue
		}
		for _, rawCitation := range sourceSlice(item, "citations") {
			citation, ok := rawCitation.(map[string]any)
			if !ok {
				continue
			}
			if updated := c.addSearchResultSource(output, query, 0, citation, 40, true); updated != nil {
				changed = append(changed, updated)
			}
		}
	}
	return changed
}

func (c *sourceCollector) addFetchOutput(output toolOutputEvent, result any) []map[string]any {
	data := mapFromAny(result)
	if data == nil {
		return nil
	}
	source := sourceObservation{
		URL:         firstSourceString(sourceString(data, "final_url", "finalUrl"), sourceString(data, "url")),
		Title:       sourceString(data, "title"),
		Description: firstSourceString(sourceDescriptionString(data), firstStringFromSlice(data["highlights"])),
		SiteName:    sourceString(data, "siteName", "site_name", "source"),
		FaviconURL:  sourceFaviconString(data),
		ImageURL:    sourceString(data, "image", "imageUrl", "image_url"),
		PublishedAt: sourceString(data, "published", "publishedAt", "published_at", "publishedDate", "datePublished", "date"),
		Priority:    100,
		Appearance: sourceAppearance{
			Kind:       "fetch",
			ToolCallID: output.ID,
			ToolName:   "fetch",
		},
	}
	if updated := c.add(source); updated != nil {
		return []map[string]any{updated}
	}
	return nil
}

func (c *sourceCollector) addProviderSources(message any) []map[string]any {
	changed := []map[string]any{}
	if typed, ok := message.(ai.Message); ok {
		for _, citation := range typed.Citations {
			if updated := c.add(providerCitationObservation(citation)); updated != nil {
				changed = append(changed, updated)
			}
		}
	}
	if typed, ok := message.(*ai.Message); ok && typed != nil {
		for _, citation := range typed.Citations {
			if updated := c.add(providerCitationObservation(citation)); updated != nil {
				changed = append(changed, updated)
			}
		}
	}
	walkProviderSources(message, func(source sourceObservation) {
		source.Priority = 80
		source.Appearance.Kind = "provider"
		source.Appearance.Cited = true
		if updated := c.add(source); updated != nil {
			changed = append(changed, updated)
		}
	})
	return changed
}

func (c *sourceCollector) addAnswerURLSources(message ai.Message) []map[string]any {
	changed := []map[string]any{}
	for _, rawURL := range extractMessageURLs(message) {
		obs := sourceObservation{
			URL:      rawURL,
			Priority: 70,
			Appearance: sourceAppearance{
				Kind:  "answer",
				Cited: true,
			},
		}
		if updated := c.add(obs); updated != nil {
			changed = append(changed, updated)
		}
	}
	return changed
}

func providerCitationObservation(citation ai.Citation) sourceObservation {
	return sourceObservation{
		URL:         citation.URL,
		Title:       citation.Title,
		Description: citation.Description,
		SiteName:    citation.SiteName,
		FaviconURL:  citation.FaviconURL,
		ImageURL:    citation.ImageURL,
		PublishedAt: citation.PublishedAt,
		Priority:    80,
		Appearance: sourceAppearance{
			Kind:         "provider",
			Cited:        true,
			StartIndex:   citation.StartIndex,
			EndIndex:     citation.EndIndex,
			ContentIndex: citation.ContentIndex,
			Text:         citation.Text,
		},
	}
}

func (c *sourceCollector) add(obs sourceObservation) map[string]any {
	normalized, ok := normalizeSourceURL(obs.URL)
	if !ok {
		return nil
	}
	source := c.byID[normalized]
	if source == nil {
		siteName := sourceSiteName(normalized, obs.SiteName)
		favicon := firstSourceString(obs.FaviconURL, sourceFallbackFaviconURL(normalized))
		source = &canonicalSource{
			SourceID:    normalized,
			URL:         normalized,
			Title:       sourceFallbackTitle(normalized, obs.Title),
			Description: sourceFallbackDescription(siteName, obs.Description),
			SiteName:    siteName,
			FaviconURL:  favicon,
			ImageURL:    firstSourceString(obs.ImageURL, favicon),
			fieldScore:  map[string]int{},
			seen:        map[string]struct{}{},
		}
		c.byID[normalized] = source
		c.order = append(c.order, normalized)
	}
	changed := false
	if source.set("title", obs.Title, obs.Priority) {
		changed = true
	}
	if source.set("description", obs.Description, obs.Priority) {
		changed = true
	}
	if source.set("siteName", obs.SiteName, obs.Priority) {
		changed = true
	}
	if source.set("faviconUrl", obs.FaviconURL, obs.Priority) {
		changed = true
	}
	if source.set("imageUrl", obs.ImageURL, obs.Priority) {
		changed = true
	}
	if source.set("publishedAt", obs.PublishedAt, obs.Priority) {
		changed = true
	}
	if source.addAppearance(obs.Appearance) {
		changed = true
	}
	if !changed {
		return nil
	}
	source.fillFallbacks()
	return source.mapValue()
}

func (s *canonicalSource) set(field string, value string, score int) bool {
	if field == "title" || field == "description" || field == "siteName" {
		value = sourceCleanText(value)
	} else {
		value = strings.TrimSpace(value)
	}
	if value == "" {
		return false
	}
	if score < s.fieldScore[field] {
		return false
	}
	switch field {
	case "title":
		if s.Title == value {
			if score > s.fieldScore[field] {
				s.fieldScore[field] = score
			}
			return false
		}
		s.Title = value
	case "description":
		if s.Description == value {
			if score > s.fieldScore[field] {
				s.fieldScore[field] = score
			}
			return false
		}
		s.Description = value
	case "siteName":
		if s.SiteName == value {
			if score > s.fieldScore[field] {
				s.fieldScore[field] = score
			}
			return false
		}
		s.SiteName = value
	case "faviconUrl":
		if s.FaviconURL == value {
			if score > s.fieldScore[field] {
				s.fieldScore[field] = score
			}
			return false
		}
		s.FaviconURL = value
	case "imageUrl":
		if s.ImageURL == value {
			if score > s.fieldScore[field] {
				s.fieldScore[field] = score
			}
			return false
		}
		s.ImageURL = value
	case "publishedAt":
		if s.PublishedAt == value {
			if score > s.fieldScore[field] {
				s.fieldScore[field] = score
			}
			return false
		}
		s.PublishedAt = value
	}
	s.fieldScore[field] = score
	return true
}

func (s *canonicalSource) addAppearance(appearance sourceAppearance) bool {
	if appearance.Kind == "" {
		return false
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%d|%t|%s|%s|%s|%s", appearance.Kind, appearance.ToolCallID, appearance.ToolName, appearance.Query, appearance.Rank, appearance.Cited, intPtrKey(appearance.StartIndex), intPtrKey(appearance.EndIndex), intPtrKey(appearance.ContentIndex), appearance.Text)
	if _, exists := s.seen[key]; exists {
		return false
	}
	s.seen[key] = struct{}{}
	s.Appearances = append(s.Appearances, appearance)
	return true
}

func (s *canonicalSource) fillFallbacks() {
	s.SiteName = sourceSiteName(s.URL, s.SiteName)
	s.FaviconURL = firstSourceString(s.FaviconURL, sourceFallbackFaviconURL(s.URL))
	s.Title = sourceFallbackTitle(s.URL, s.Title)
	s.Description = sourceFallbackDescription(s.SiteName, s.Description)
	s.ImageURL = firstSourceString(s.ImageURL, s.FaviconURL)
}

func (s *canonicalSource) mapValue() map[string]any {
	out := map[string]any{
		"sourceId":    s.SourceID,
		"url":         s.URL,
		"title":       s.Title,
		"description": s.Description,
		"siteName":    s.SiteName,
		"faviconUrl":  s.FaviconURL,
		"imageUrl":    s.ImageURL,
		"appearances": sourceAppearances(s.Appearances),
	}
	if s.PublishedAt != "" {
		out["publishedAt"] = s.PublishedAt
	}
	return out
}

func (c *sourceCollector) sources() []map[string]any {
	out := make([]map[string]any, 0, len(c.order))
	for _, id := range c.order {
		if source := c.byID[id]; source != nil {
			source.fillFallbacks()
			out = append(out, source.mapValue())
		}
	}
	return out
}

var markdownURLPattern = regexp.MustCompile(`https?://[^\s<>"'\]\)]+`)

func extractMessageURLs(message ai.Message) []string {
	text := strings.TrimSpace(messageTextContent(message.Content))
	if text == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, match := range markdownURLPattern.FindAllString(text, -1) {
		match = strings.TrimRight(match, ".,;:!?")
		normalized, ok := normalizeSourceURL(match)
		if !ok || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func messageTextContent(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []ai.ContentBlock:
		var parts []string
		for _, block := range typed {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	case []any:
		var parts []string
		for _, item := range typed {
			if block, ok := item.(map[string]any); ok && stringFromAny(block["type"]) == "text" {
				if text := stringFromAny(block["text"]); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func sourceAppearances(values []sourceAppearance) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item := map[string]any{"kind": value.Kind}
		if value.ToolCallID != "" {
			item["toolCallId"] = value.ToolCallID
		}
		if value.ToolName != "" {
			item["toolName"] = value.ToolName
		}
		if value.Query != "" {
			item["query"] = value.Query
		}
		if value.Rank > 0 {
			item["rank"] = value.Rank
		}
		if value.Cited {
			item["cited"] = true
		}
		if value.StartIndex != nil {
			item["startIndex"] = *value.StartIndex
		}
		if value.EndIndex != nil {
			item["endIndex"] = *value.EndIndex
		}
		if value.ContentIndex != nil {
			item["contentIndex"] = *value.ContentIndex
		}
		if value.Text != "" {
			item["text"] = value.Text
		}
		out = append(out, item)
	}
	return out
}

func intPtrKey(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func normalizeSourceURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" || lower == "mc_cid" || lower == "mc_eid" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), true
}

func walkProviderSources(value any, emit func(sourceObservation)) {
	switch typed := value.(type) {
	case nil:
		return
	case map[string]any:
		if source, ok := providerCitationSource(typed); ok {
			emit(source)
		}
		for key, item := range typed {
			lower := strings.ToLower(key)
			if lower == "annotations" || lower == "citations" || lower == "citation" || lower == "url_citation" || lower == "urlcitation" {
				walkProviderSources(item, emit)
				continue
			}
			switch item.(type) {
			case map[string]any, []any:
				walkProviderSources(item, emit)
			}
		}
	case []any:
		for _, item := range typed {
			walkProviderSources(item, emit)
		}
	default:
		data := mapFromAny(value)
		if data != nil {
			walkProviderSources(data, emit)
		}
	}
}

func providerCitationSource(data map[string]any) (sourceObservation, bool) {
	sourceType := strings.ToLower(sourceString(data, "type", "rawType"))
	nested, _ := data["url_citation"].(map[string]any)
	if nested == nil {
		nested, _ = data["urlCitation"].(map[string]any)
	}
	citation := data
	if nested != nil {
		citation = mergeSourceMaps(data, nested)
		sourceType = firstSourceString(sourceType, strings.ToLower(sourceString(nested, "type", "rawType")))
	}
	rawURL := sourceString(citation, "url", "uri")
	if rawURL == "" || (!strings.Contains(sourceType, "citation") && sourceType != "web_search_result_location") {
		return sourceObservation{}, false
	}
	return sourceObservation{
		URL:         rawURL,
		Title:       sourceString(citation, "title"),
		Description: firstSourceString(sourceDescriptionString(citation), sourceString(citation, "cited_text"), sourceString(citation, "text")),
		SiteName:    sourceString(citation, "siteName", "site_name"),
		FaviconURL:  sourceFaviconString(citation),
		ImageURL:    sourceImageString(citation),
		PublishedAt: sourceString(citation, "published", "publishedAt", "published_at", "publishedDate", "datePublished", "date"),
		Appearance: sourceAppearance{
			Kind:         "provider",
			Cited:        true,
			StartIndex:   intPointerFromAny(firstSourceAny(citation, "startIndex", "start_index")),
			EndIndex:     intPointerFromAny(firstSourceAny(citation, "endIndex", "end_index")),
			ContentIndex: intPointerFromAny(firstSourceAny(citation, "contentIndex", "content_index", "outputIndex", "output_index")),
			Text:         firstSourceString(sourceString(citation, "text"), sourceString(citation, "cited_text")),
		},
	}, true
}

func mergeSourceMaps(first map[string]any, second map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range first {
		out[key] = value
	}
	for key, value := range second {
		out[key] = value
	}
	return out
}

func firstSourceAny(data map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value
		}
	}
	return nil
}

func intPointerFromAny(value any) *int {
	var out int
	switch typed := value.(type) {
	case int:
		out = typed
	case int64:
		out = int(typed)
	case float64:
		out = int(typed)
	case string:
		if typed = strings.TrimSpace(typed); typed == "" {
			return nil
		}
		if _, err := fmt.Sscanf(typed, "%d", &out); err != nil {
			return nil
		}
	default:
		return nil
	}
	return &out
}

func sourceString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringFromAny(data[key])); value != "" {
			return value
		}
	}
	return ""
}

func sourceSlice(data map[string]any, keys ...string) []any {
	for _, key := range keys {
		switch typed := data[key].(type) {
		case []any:
			if len(typed) > 0 {
				return typed
			}
		case []string:
			if len(typed) == 0 {
				continue
			}
			out := make([]any, 0, len(typed))
			for _, value := range typed {
				out = append(out, value)
			}
			return out
		}
	}
	return nil
}

func sourceDescriptionString(data map[string]any) string {
	if data == nil {
		return ""
	}
	if value := sourceString(data, "description", "snippet", "summary", "ogDescription", "openGraphDescription", "twitterDescription", "twitter_description"); value != "" {
		return value
	}
	for _, key := range []string{"metadata", "meta", "openGraph", "open_graph", "og", "twitter"} {
		if nested, ok := data[key].(map[string]any); ok {
			if value := sourceDescriptionString(nested); value != "" {
				return value
			}
		}
	}
	return ""
}

func sourceFaviconString(data map[string]any) string {
	if data == nil {
		return ""
	}
	if value := sourceString(data, "favicon", "faviconUrl", "favicon_url", "icon", "iconUrl", "icon_url", "siteIcon", "site_icon", "appleTouchIcon", "apple_touch_icon"); value != "" {
		return value
	}
	for _, key := range []string{"metadata", "meta", "openGraph", "open_graph", "og", "twitter"} {
		if nested, ok := data[key].(map[string]any); ok {
			if value := sourceFaviconString(nested); value != "" {
				return value
			}
		}
	}
	return ""
}

func sourceImageString(data map[string]any) string {
	if image, ok := data["image"].(map[string]any); ok {
		if value := sourceString(image, "url"); value != "" {
			return value
		}
	}
	if value := sourceString(data, "image", "imageUrl", "image_url", "thumbnail", "thumbnailUrl", "thumbnail_url", "ogImage", "openGraphImage"); value != "" {
		return value
	}
	if extras, ok := data["extras"].(map[string]any); ok {
		if value := firstStringFromSlice(sourceSlice(extras, "imageLinks", "image_links", "images")); value != "" {
			return value
		}
	}
	return firstStringFromSlice(sourceSlice(data, "imageLinks", "image_links", "images"))
}

func firstSourceString(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func firstStringFromSlice(value any) string {
	switch typed := value.(type) {
	case []string:
		if len(typed) > 0 {
			return typed[0]
		}
	case []any:
		for _, item := range typed {
			if text := sourceCleanText(stringFromAny(item)); text != "" {
				return text
			}
		}
	}
	return ""
}

func sourceCleanText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > 220 {
		return string(runes[:220]) + "..."
	}
	return value
}

func sourceSiteName(rawURL string, fallback string) string {
	if fallback = sourceCleanText(fallback); fallback != "" {
		return fallback
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return rawURL
	}
	return parsed.Hostname()
}

func sourceFallbackTitle(rawURL string, fallback string) string {
	if fallback = sourceCleanText(fallback); fallback != "" {
		return fallback
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return rawURL
	}
	if path := strings.Trim(parsed.EscapedPath(), "/"); path != "" {
		parts := strings.Split(path, "/")
		return parts[len(parts)-1]
	}
	return parsed.Hostname()
}

func sourceFallbackDescription(siteName string, fallback string) string {
	if fallback = sourceCleanText(fallback); fallback != "" {
		return fallback
	}
	return "Source from " + siteName
}

func sourceFallbackFaviconURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/favicon.ico"}).String()
}
