package chattools

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
var scriptStyleRE = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
var tagRE = regexp.MustCompile(`(?is)<[^>]+>`)
var whitespaceRE = regexp.MustCompile(`\s+`)

type htmlMetadata struct {
	Title       string
	Description string
	Favicon     string
}

func extractText(body []byte, contentType string) string {
	raw := string(body)
	if strings.Contains(strings.ToLower(contentType), "html") || strings.Contains(raw, "<html") {
		raw = scriptStyleRE.ReplaceAllString(raw, " ")
		raw = tagRE.ReplaceAllString(raw, " ")
	}
	return cleanText(raw)
}

func extractHTMLMetadata(body []byte, baseURL *url.URL) htmlMetadata {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return htmlMetadata{Title: extractTitleFallback(body)}
	}
	var out htmlMetadata
	descriptionScore := 0
	faviconScore := 0
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch strings.ToLower(node.Data) {
			case "title":
				if out.Title == "" {
					out.Title = cleanText(nodeText(node))
				}
			case "meta":
				score := descriptionPriority(node)
				if score > descriptionScore {
					if content := cleanText(attr(node, "content")); content != "" {
						out.Description = content
						descriptionScore = score
					}
				}
			case "link":
				score := faviconPriority(node)
				if score > faviconScore {
					if href := resolveMetadataURL(attr(node, "href"), baseURL); href != "" {
						out.Favicon = href
						faviconScore = score
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if out.Title == "" {
		out.Title = extractTitleFallback(body)
	}
	return out
}

func extractTitleFallback(body []byte) string {
	match := titleRE.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return cleanText(string(match[1]))
}

func nodeText(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func attr(node *html.Node, name string) string {
	for _, value := range node.Attr {
		if strings.EqualFold(value.Key, name) {
			return value.Val
		}
	}
	return ""
}

func descriptionPriority(node *html.Node) int {
	if strings.EqualFold(attr(node, "name"), "description") {
		return 100
	}
	if strings.EqualFold(attr(node, "property"), "og:description") {
		return 90
	}
	if strings.EqualFold(attr(node, "name"), "twitter:description") {
		return 80
	}
	if strings.EqualFold(attr(node, "itemprop"), "description") {
		return 70
	}
	return 0
}

func faviconPriority(node *html.Node) int {
	rel := strings.ToLower(attr(node, "rel"))
	if !strings.Contains(rel, "icon") {
		return 0
	}
	if strings.Contains(rel, "apple-touch-icon") {
		return 70
	}
	if strings.Contains(rel, "shortcut") {
		return 90
	}
	return 100
}

func resolveMetadataURL(raw string, baseURL *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return ""
	}
	if baseURL != nil {
		parsed = baseURL.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = whitespaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}
