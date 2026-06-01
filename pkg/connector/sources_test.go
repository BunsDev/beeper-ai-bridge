package connector

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestSourceCollectorUsesDescriptionAndFaviconFallbacks(t *testing.T) {
	webSources := newSourceCollector().addWebSearchOutput(toolOutputEvent{
		ID:    "call-search",
		Name:  "web_search",
		Input: map[string]any{"query": "q"},
	}, map[string]any{
		"results": []any{
			map[string]any{
				"title": "Search Result",
				"url":   "https://example.com/search",
				"metadata": map[string]any{
					"openGraphDescription": "Open Graph description",
					"iconUrl":              "https://example.com/icon.png",
				},
				"extras": map[string]any{"imageLinks": []any{"https://example.com/page-image.png"}},
				"subpages": []any{map[string]any{
					"title":   "Subpage Result",
					"url":     "https://example.com/subpage",
					"summary": "Subpage summary",
					"favicon": "https://example.com/subpage.ico",
				}},
			},
		},
		"output": map[string]any{
			"grounding": []any{map[string]any{
				"field": "content",
				"citations": []any{map[string]any{
					"title": "Grounded Result",
					"url":   "https://example.com/grounded",
				}},
			}},
		},
	})
	if len(webSources) != 3 {
		t.Fatalf("web search helper should still normalize sources for grounded/provider-like payloads, got %#v", webSources)
	}

	fetchSources := newSourceCollector().addFetchOutput(toolOutputEvent{
		ID:   "call-fetch",
		Name: "fetch",
	}, map[string]any{
		"final_url":   "https://example.com/fetched",
		"title":       "Fetched Result",
		"description": "Fetched description",
		"favicon":     "https://example.com/favicon.ico",
	})
	if len(fetchSources) != 1 || fetchSources[0]["description"] != "Fetched description" || fetchSources[0]["faviconUrl"] != "https://example.com/favicon.ico" {
		t.Fatalf("fetch source metadata fallback failed: %#v", fetchSources)
	}

	captchaSources := newSourceCollector().addFetchOutput(toolOutputEvent{
		ID:   "call-fetch-captcha",
		Name: "fetch",
	}, map[string]any{
		"final_url": "https://example.com/world",
		"text":      `{"url":"https://geo.captcha-delivery.com/captcha/?cid=long"}`,
	})
	if len(captchaSources) != 1 || captchaSources[0]["description"] != "Source from example.com" {
		t.Fatalf("fetch source should not use full page text as card description: %#v", captchaSources)
	}

	providerSources := newSourceCollector().addProviderSources(map[string]any{
		"annotations": []any{map[string]any{
			"type":        "url_citation",
			"url":         "https://example.com/provider",
			"start_index": float64(10),
			"end_index":   float64(20),
			"metadata": map[string]any{
				"twitterDescription": "Provider description",
				"siteIcon":           "https://example.com/provider.ico",
			},
		}},
	})
	if len(providerSources) != 1 || providerSources[0]["description"] != "Provider description" || providerSources[0]["faviconUrl"] != "https://example.com/provider.ico" {
		t.Fatalf("provider source metadata fallback failed: %#v", providerSources)
	}
	appearances, ok := providerSources[0]["appearances"].([]map[string]any)
	if !ok || len(appearances) != 1 || appearances[0]["startIndex"] != 10 || appearances[0]["endIndex"] != 20 {
		t.Fatalf("provider citation ranges were not preserved: %#v", providerSources[0])
	}

	fetchResultSources := newSourceCollector().addProviderSources(map[string]any{
		"type": "web_fetch_tool_result",
		"content": map[string]any{
			"type": "web_fetch_result",
			"url":  "https://example.com/provider-fetch",
			"content": map[string]any{
				"type":  "document",
				"title": "Provider Fetch",
			},
		},
	})
	if len(fetchResultSources) != 1 || fetchResultSources[0]["title"] != "Provider Fetch" {
		t.Fatalf("provider web fetch result was not mapped: %#v", fetchResultSources)
	}

	messageSources := newSourceCollector().addProviderSources(ai.Message{
		Citations: []ai.Citation{{
			Type:       "url_citation",
			URL:        "https://example.com/message-citation",
			Title:      "Typed Citation",
			StartIndex: intPtr(30),
			EndIndex:   intPtr(40),
		}},
	})
	if len(messageSources) != 1 || messageSources[0]["title"] != "Typed Citation" {
		t.Fatalf("typed provider citation was not mapped: %#v", messageSources)
	}

	urlContextSources := newSourceCollector().addProviderSources(ai.Message{
		Citations: []ai.Citation{{
			Type:    "url_citation",
			URL:     "https://example.com/url-context",
			RawType: "url_context",
		}},
	})
	if len(urlContextSources) != 1 || urlContextSources[0]["url"] != "https://example.com/url-context" {
		t.Fatalf("Google URL context source was not mapped: %#v", urlContextSources)
	}

	collector := newSourceCollector()
	searchSources := collector.addWebSearchOutput(toolOutputEvent{
		ID:    "call-search",
		Name:  "web_search",
		Input: map[string]any{"query": "q"},
	}, map[string]any{
		"results": []any{map[string]any{
			"title": "Known Result",
			"url":   "https://example.com/known?utm_source=noise",
		}},
	})
	if len(searchSources) != 1 {
		t.Fatalf("expected search source, got %#v", searchSources)
	}
	answerSources := collector.addAnswerURLSources(ai.Message{Content: "Use https://example.com/known and https://example.org/new."})
	if len(answerSources) != 2 {
		t.Fatalf("expected answer URL sources, got %#v", answerSources)
	}
	sources := collector.sources()
	if len(sources) != 2 {
		t.Fatalf("expected canonical known + new sources, got %#v", sources)
	}
	answerAppearances, ok := sources[0]["appearances"].([]map[string]any)
	if !ok || len(answerAppearances) != 2 || answerAppearances[1]["kind"] != "answer" || answerAppearances[1]["cited"] != true {
		t.Fatalf("known source was not marked cited by final answer URL: %#v", sources[0])
	}
}

func intPtr(value int) *int {
	return &value
}
