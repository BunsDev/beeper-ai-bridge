package utils

import (
	"context"

	harness "github.com/beeper/ai-bridge/pkg/agent/harness"
)

type ShellCaptureOptions = harness.ShellCaptureOptions
type ShellCaptureResult = harness.ShellCaptureResult

func SanitizeBinaryOutput(str string) string {
	return harness.SanitizeBinaryOutput(str)
}

func ExecuteShellWithCapture(ctx context.Context, env *harness.LocalExecutionEnv, command string, options ShellCaptureOptions) (ShellCaptureResult, error) {
	return harness.ExecuteShellWithCapture(ctx, env, command, options)
}
