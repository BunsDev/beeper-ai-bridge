package harness

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type ShellCaptureOptions struct {
	ExecOptions
	OnChunk func(string)
}

type ShellCaptureResult struct {
	Output         string
	ExitCode       *int
	Cancelled      bool
	Truncated      bool
	FullOutputPath string
}

func SanitizeBinaryOutput(str string) string {
	var builder strings.Builder
	for _, char := range str {
		code := char
		if code == 0x09 || code == 0x0a || code == 0x0d {
			builder.WriteRune(char)
			continue
		}
		if code <= 0x1f {
			continue
		}
		if code >= 0xfff9 && code <= 0xfffb {
			continue
		}
		if char == utf8.RuneError {
			builder.WriteRune(char)
			continue
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

func ExecuteShellWithCapture(ctx context.Context, env *LocalExecutionEnv, command string, options ShellCaptureOptions) (ShellCaptureResult, error) {
	outputChunks := []string{}
	outputBytes := 0
	maxOutputBytes := DefaultMaxBytes * 2
	totalBytes := 0
	fullOutputPath := ""
	var captureErr error
	appendChunk := func(chunk string) {
		totalBytes += len([]byte(chunk))
		text := strings.ReplaceAll(SanitizeBinaryOutput(chunk), "\r", "")
		if captureErr == nil && totalBytes > DefaultMaxBytes && fullOutputPath == "" {
			path, err := env.CreateTempFile("bash-", ".log")
			if err != nil {
				captureErr = fmt.Errorf("create full output file: %w", err)
			} else {
				fullOutputPath = path
				if err := env.AppendFile(path, []byte(strings.Join(outputChunks, "")+text)); err != nil {
					captureErr = fmt.Errorf("append full output: %w", err)
				}
			}
		} else if captureErr == nil && fullOutputPath != "" {
			if err := env.AppendFile(fullOutputPath, []byte(text)); err != nil {
				captureErr = fmt.Errorf("append full output: %w", err)
			}
		}
		outputChunks = append(outputChunks, text)
		outputBytes += len(text)
		for outputBytes > maxOutputBytes && len(outputChunks) > 1 {
			removed := outputChunks[0]
			outputChunks = outputChunks[1:]
			outputBytes -= len(removed)
		}
		if options.OnChunk != nil {
			options.OnChunk(text)
		}
	}
	execOptions := options.ExecOptions
	execOptions.OnStdout = appendChunk
	execOptions.OnStderr = appendChunk
	result, err := env.Exec(ctx, command, execOptions)
	tailOutput := strings.Join(outputChunks, "")
	truncation := TruncateTail(tailOutput, TruncationOptions{})
	output := tailOutput
	if truncation.Truncated {
		output = truncation.Content
		if fullOutputPath == "" {
			path, createErr := env.CreateTempFile("bash-", ".log")
			if createErr != nil {
				captureErr = fmt.Errorf("create full output file: %w", createErr)
			} else {
				fullOutputPath = path
				if err := env.AppendFile(path, []byte(tailOutput)); err != nil {
					captureErr = fmt.Errorf("append full output: %w", err)
				}
			}
		}
	}
	if captureErr != nil {
		return ShellCaptureResult{}, toExecutionError(captureErr)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ShellCaptureResult{Output: output, Cancelled: true, Truncated: truncation.Truncated, FullOutputPath: fullOutputPath}, nil
		}
		return ShellCaptureResult{}, err
	}
	exitCode := result.ExitCode
	return ShellCaptureResult{Output: output, ExitCode: &exitCode, Cancelled: false, Truncated: truncation.Truncated, FullOutputPath: fullOutputPath}, nil
}
