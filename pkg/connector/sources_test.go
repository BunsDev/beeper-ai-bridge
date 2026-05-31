package connector

import "testing"

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
		t.Fatalf("web source metadata fallback failed: %#v", webSources)
	}
	if webSources[0]["description"] != "Open Graph description" || webSources[0]["faviconUrl"] != "https://example.com/icon.png" || webSources[0]["imageUrl"] != "https://example.com/page-image.png" {
		t.Fatalf("web source metadata fallback failed: %#v", webSources[0])
	}
	if webSources[1]["url"] != "https://example.com/subpage" || webSources[1]["description"] != "Subpage summary" || webSources[1]["faviconUrl"] != "https://example.com/subpage.ico" {
		t.Fatalf("web subpage source metadata fallback failed: %#v", webSources[1])
	}
	appearances, ok := webSources[2]["appearances"].([]map[string]any)
	if webSources[2]["url"] != "https://example.com/grounded" || !ok || len(appearances) != 1 || !appearances[0]["cited"].(bool) {
		t.Fatalf("web grounding source metadata fallback failed: %#v", webSources[2])
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

	providerSources := newSourceCollector().addProviderSources(map[string]any{
		"annotations": []any{map[string]any{
			"type": "url_citation",
			"url":  "https://example.com/provider",
			"metadata": map[string]any{
				"twitterDescription": "Provider description",
				"siteIcon":           "https://example.com/provider.ico",
			},
		}},
	})
	if len(providerSources) != 1 || providerSources[0]["description"] != "Provider description" || providerSources[0]["faviconUrl"] != "https://example.com/provider.ico" {
		t.Fatalf("provider source metadata fallback failed: %#v", providerSources)
	}
}
