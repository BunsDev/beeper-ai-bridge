package utils

import harness "github.com/earendil-works/pi-mono/packages/agent/src/harness"

const (
	DefaultMaxLines   = harness.DefaultMaxLines
	DefaultMaxBytes   = harness.DefaultMaxBytes
	GrepMaxLineLength = harness.GrepMaxLineLength
)

type TruncationOptions = harness.TruncationOptions
type TruncationResult = harness.TruncationResult

func FormatSize(bytes int) string {
	return harness.FormatSize(bytes)
}

func TruncateHead(content string, options TruncationOptions) TruncationResult {
	return harness.TruncateHead(content, options)
}

func TruncateTail(content string, options TruncationOptions) TruncationResult {
	return harness.TruncateTail(content, options)
}

func TruncateLine(line string, maxChars int) (string, bool) {
	return harness.TruncateLine(line, maxChars)
}
