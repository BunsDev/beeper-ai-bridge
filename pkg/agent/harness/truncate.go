package harness

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const DefaultMaxLines = 2000
const DefaultMaxBytes = 50 * 1024
const GrepMaxLineLength = 500

type TruncationOptions struct {
	MaxLines int
	MaxBytes int
}

type TruncationResult struct {
	Content               string
	Truncated             bool
	TruncatedBy           string
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

func FormatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

func TruncateHead(content string, options TruncationOptions) TruncationResult {
	maxLines, maxBytes := truncationLimits(options)
	totalBytes := len([]byte(content))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return noTruncation(content, totalLines, totalBytes, maxLines, maxBytes)
	}
	if len([]byte(lines[0])) > maxBytes {
		return TruncationResult{Truncated: true, TruncatedBy: "bytes", TotalLines: totalLines, TotalBytes: totalBytes, FirstLineExceedsLimit: true, MaxLines: maxLines, MaxBytes: maxBytes}
	}
	output := []string{}
	outputBytes := 0
	truncatedBy := "lines"
	for i := 0; i < len(lines) && i < maxLines; i++ {
		lineBytes := len([]byte(lines[i]))
		if i > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		output = append(output, lines[i])
		outputBytes += lineBytes
	}
	contentOut := strings.Join(output, "\n")
	return TruncationResult{Content: contentOut, Truncated: true, TruncatedBy: truncatedBy, TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: len(output), OutputBytes: len([]byte(contentOut)), MaxLines: maxLines, MaxBytes: maxBytes}
}

func TruncateTail(content string, options TruncationOptions) TruncationResult {
	maxLines, maxBytes := truncationLimits(options)
	totalBytes := len([]byte(content))
	lines := strings.Split(content, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return noTruncation(content, totalLines, totalBytes, maxLines, maxBytes)
	}
	output := []string{}
	outputBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false
	for i := len(lines) - 1; i >= 0 && len(output) < maxLines; i-- {
		lineBytes := len([]byte(lines[i]))
		if len(output) > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(output) == 0 {
				line := truncateStringToBytesFromEnd(lines[i], maxBytes)
				output = append([]string{line}, output...)
				lastLinePartial = true
			}
			break
		}
		output = append([]string{lines[i]}, output...)
		outputBytes += lineBytes
	}
	contentOut := strings.Join(output, "\n")
	return TruncationResult{Content: contentOut, Truncated: true, TruncatedBy: truncatedBy, TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: len(output), OutputBytes: len([]byte(contentOut)), LastLinePartial: lastLinePartial, MaxLines: maxLines, MaxBytes: maxBytes}
}

func TruncateLine(line string, maxChars int) (string, bool) {
	if maxChars == 0 {
		maxChars = GrepMaxLineLength
	}
	if len([]rune(line)) <= maxChars {
		return line, false
	}
	runes := []rune(line)
	return string(runes[:maxChars]) + "... [truncated]", true
}

func truncationLimits(options TruncationOptions) (int, int) {
	maxLines := options.MaxLines
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	return maxLines, maxBytes
}

func noTruncation(content string, totalLines int, totalBytes int, maxLines int, maxBytes int) TruncationResult {
	return TruncationResult{Content: content, TotalLines: totalLines, TotalBytes: totalBytes, OutputLines: totalLines, OutputBytes: totalBytes, MaxLines: maxLines, MaxBytes: maxBytes}
}

func truncateStringToBytesFromEnd(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	start := len(value)
	for start > 0 {
		nextStart := start - 1
		for nextStart > 0 && !utf8.RuneStart(value[nextStart]) {
			nextStart--
		}
		if len([]byte(value[nextStart:])) > maxBytes {
			break
		}
		start = nextStart
	}
	return value[start:]
}
