package providers

import (
	"fmt"
	"strconv"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func providerCitationsFromAny(value any, provider ai.Provider, contentIndex int) []ai.Citation {
	out := []ai.Citation{}
	if data, ok := value.(map[string]any); ok {
		out = append(out, googleGroundingCitationsFromMap(data, provider, contentIndex)...)
	}
	walkProviderCitationMaps(value, func(item map[string]any) {
		if citation, ok := providerCitationFromMap(item, provider, contentIndex); ok {
			out = append(out, citation)
		}
	})
	return out
}

func googleGroundingCitationsFromMap(data map[string]any, provider ai.Provider, contentIndex int) []ai.Citation {
	metadata, _ := data["groundingMetadata"].(map[string]any)
	if metadata == nil {
		metadata, _ = data["grounding_metadata"].(map[string]any)
	}
	if metadata == nil {
		if candidates, _ := data["candidates"].([]any); len(candidates) > 0 {
			if candidate, _ := candidates[0].(map[string]any); candidate != nil {
				return googleGroundingCitationsFromMap(candidate, provider, contentIndex)
			}
		}
		return nil
	}
	chunks, _ := metadata["groundingChunks"].([]any)
	if chunks == nil {
		chunks, _ = metadata["grounding_chunks"].([]any)
	}
	if len(chunks) == 0 {
		return nil
	}
	chunkCitations := make([]ai.Citation, 0, len(chunks))
	for _, rawChunk := range chunks {
		chunk, _ := rawChunk.(map[string]any)
		web, _ := chunk["web"].(map[string]any)
		if web == nil {
			web, _ = chunk["retrievedContext"].(map[string]any)
		}
		if web == nil {
			chunkCitations = append(chunkCitations, ai.Citation{})
			continue
		}
		url := firstCitationString(stringFromAny(web["uri"]), stringFromAny(web["url"]))
		chunkCitations = append(chunkCitations, ai.Citation{
			Type:         "url_citation",
			URL:          url,
			Title:        stringFromAny(web["title"]),
			ContentIndex: &contentIndex,
			Provider:     string(provider),
			RawType:      "grounding",
		})
	}
	supports, _ := metadata["groundingSupports"].([]any)
	if supports == nil {
		supports, _ = metadata["grounding_supports"].([]any)
	}
	if len(supports) == 0 {
		out := []ai.Citation{}
		for _, citation := range chunkCitations {
			if citation.URL != "" {
				out = append(out, citation)
			}
		}
		return out
	}
	out := []ai.Citation{}
	for _, rawSupport := range supports {
		support, _ := rawSupport.(map[string]any)
		segment, _ := support["segment"].(map[string]any)
		indices := citationIndexList(firstCitationAny(support, "groundingChunkIndices", "grounding_chunk_indices"))
		for _, index := range indices {
			if index < 0 || index >= len(chunkCitations) || chunkCitations[index].URL == "" {
				continue
			}
			citation := chunkCitations[index]
			if start, ok := intFromCitationAny(firstCitationAny(segment, "startIndex", "start_index")); ok {
				citation.StartIndex = &start
			}
			if end, ok := intFromCitationAny(firstCitationAny(segment, "endIndex", "end_index")); ok {
				citation.EndIndex = &end
			}
			citation.Text = stringFromAny(segment["text"])
			out = append(out, citation)
		}
	}
	return out
}

func citationIndexList(value any) []int {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, item := range raw {
		if index, ok := intFromCitationAny(item); ok {
			out = append(out, index)
		}
	}
	return out
}

func walkProviderCitationMaps(value any, emit func(map[string]any)) {
	switch typed := value.(type) {
	case nil:
		return
	case map[string]any:
		emit(typed)
		for _, item := range typed {
			switch item.(type) {
			case map[string]any, []any:
				walkProviderCitationMaps(item, emit)
			}
		}
	case []any:
		for _, item := range typed {
			walkProviderCitationMaps(item, emit)
		}
	}
}

func providerCitationFromMap(data map[string]any, provider ai.Provider, contentIndex int) (ai.Citation, bool) {
	rawType := strings.ToLower(stringFromAny(data["type"]))
	citationData := data
	if nested, _ := data["url_citation"].(map[string]any); nested != nil {
		citationData = mergeCitationMaps(data, nested)
	} else if nested, _ := data["urlCitation"].(map[string]any); nested != nil {
		citationData = mergeCitationMaps(data, nested)
	}
	rawType = firstCitationString(rawType, strings.ToLower(stringFromAny(citationData["type"])))
	url := firstCitationString(stringFromAny(citationData["url"]), stringFromAny(citationData["uri"]))
	if url == "" || (!strings.Contains(rawType, "citation") && rawType != "web_search_result_location") {
		return ai.Citation{}, false
	}
	resolvedContentIndex := contentIndex
	if index, ok := intFromCitationAny(firstCitationAny(citationData, "contentIndex", "content_index", "outputIndex", "output_index")); ok {
		resolvedContentIndex = index
	}
	citation := ai.Citation{
		Type:         "url_citation",
		URL:          url,
		Title:        stringFromAny(citationData["title"]),
		Description:  firstCitationString(stringFromAny(citationData["description"]), stringFromAny(citationData["summary"])),
		SiteName:     firstCitationString(stringFromAny(citationData["siteName"]), stringFromAny(citationData["site_name"])),
		FaviconURL:   firstCitationString(stringFromAny(citationData["faviconUrl"]), stringFromAny(citationData["favicon_url"])),
		ImageURL:     firstCitationString(stringFromAny(citationData["imageUrl"]), stringFromAny(citationData["image_url"])),
		PublishedAt:  firstCitationString(stringFromAny(citationData["publishedAt"]), stringFromAny(citationData["published_at"]), stringFromAny(citationData["publishedDate"]), stringFromAny(citationData["datePublished"]), stringFromAny(citationData["date"])),
		ContentIndex: &resolvedContentIndex,
		Provider:     string(provider),
		RawType:      rawType,
		Text:         firstCitationString(stringFromAny(citationData["text"]), stringFromAny(citationData["cited_text"])),
	}
	if start, ok := intFromCitationAny(firstCitationAny(citationData, "startIndex", "start_index")); ok {
		citation.StartIndex = &start
	}
	if end, ok := intFromCitationAny(firstCitationAny(citationData, "endIndex", "end_index")); ok {
		citation.EndIndex = &end
	}
	return citation, true
}

func mergeCitationMaps(first map[string]any, second map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range first {
		out[key] = value
	}
	for key, value := range second {
		out[key] = value
	}
	return out
}

func firstCitationAny(data map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value
		}
	}
	return nil
}

func firstCitationString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func intFromCitationAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		if value != nil {
			if parsed, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
				return parsed, true
			}
		}
		return 0, false
	}
}
