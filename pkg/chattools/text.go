package chattools

import (
	"regexp"
	"strings"
)

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
