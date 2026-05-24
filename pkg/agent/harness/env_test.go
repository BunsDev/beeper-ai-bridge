package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalExecutionEnvFileOperations(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	if err := env.WriteFile("dir/file.txt", []byte("a\nb\nc")); err != nil {
		t.Fatal(err)
	}
	text, err := env.ReadTextFile("dir/file.txt")
	if err != nil || text != "a\nb\nc" {
		t.Fatalf("unexpected text %q err %v", text, err)
	}
	lines, err := env.ReadTextLines("dir/file.txt", 2)
	if err != nil || len(lines) != 2 || lines[1] != "b" {
		t.Fatalf("unexpected lines %#v err %v", lines, err)
	}
	info, err := env.FileInfo("dir/file.txt")
	if err != nil || info.Kind != FileKindFile || info.Name != "file.txt" {
		t.Fatalf("unexpected info %#v err %v", info, err)
	}
	exists, err := env.Exists("missing")
	if err != nil || exists {
		t.Fatalf("expected missing false, got %v err %v", exists, err)
	}
	tempFile, err := env.CreateTempFile("x-", ".log")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(tempFile) != ".log" {
		t.Fatalf("expected .log temp file, got %q", tempFile)
	}
}

func TestLocalExecutionEnvExecAndCapture(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	var chunks []string
	result, err := env.Exec(context.Background(), `printf "out"; printf "err" >&2`, ExecOptions{OnStdout: func(chunk string) { chunks = append(chunks, chunk) }})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "out" || result.Stderr != "err" || result.ExitCode != 0 {
		t.Fatalf("unexpected result %#v", result)
	}
	if len(chunks) == 0 {
		t.Fatalf("expected stdout callback")
	}

	capture, err := ExecuteShellWithCapture(context.Background(), env, `printf "a\x00b\n"`, ShellCaptureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if capture.Output != "ab\n" || capture.ExitCode == nil || *capture.ExitCode != 0 {
		t.Fatalf("unexpected capture %#v", capture)
	}
}

func TestExecuteShellWithCaptureReturnsTempFileError(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	tempFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(tempFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tempFile)

	_, err := ExecuteShellWithCapture(context.Background(), env, `i=0; while [ "$i" -lt 60000 ]; do printf x; i=$((i+1)); done`, ShellCaptureOptions{})
	if err == nil {
		t.Fatalf("expected capture error")
	}
	var execErr *ExecutionError
	if !errors.As(err, &execErr) || execErr.Code != ExecutionErrorSpawnError {
		t.Fatalf("expected spawn execution error, got %T %v", err, err)
	}
}

func TestLocalExecutionEnvExecTimeout(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	_, err := env.Exec(context.Background(), "sleep 2", ExecOptions{Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestLocalExecutionEnvResultWrappers(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	if result := env.TryWriteFile("dir/file.txt", []byte("hello")); !result.OK {
		t.Fatalf("unexpected write error %v", result.Error)
	}
	read := env.TryReadTextFile("dir/file.txt")
	if !read.OK || read.Value != "hello" {
		t.Fatalf("unexpected read result %#v", read)
	}
	missing := env.TryReadTextFile("missing.txt")
	if missing.OK || missing.Error == nil || missing.Error.Code != FileErrorNotFound {
		t.Fatalf("expected not_found result, got %#v", missing)
	}
	timeout := env.TryExec(context.Background(), "sleep 2", ExecOptions{Timeout: 10 * time.Millisecond})
	if timeout.OK || timeout.Error == nil || timeout.Error.Code != ExecutionErrorTimeout {
		t.Fatalf("expected timeout result, got %#v", timeout)
	}
	value, err := GetOrThrow(read)
	if err != nil || value != "hello" {
		t.Fatalf("unexpected GetOrThrow %q err %v", value, err)
	}
	if zero := GetOrZero(missing); zero != "" {
		t.Fatalf("expected zero value, got %q", zero)
	}
	if got := GetOrUndefined(read); got == nil || *got != "hello" {
		t.Fatalf("expected GetOrUndefined value, got %#v", got)
	}
	if got := GetOrUndefined(missing); got != nil {
		t.Fatalf("expected nil GetOrUndefined for error result, got %#v", got)
	}
}

func TestLocalExecutionEnvContextResultWrappersReturnAborted(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	if result := env.TryWriteFile("file.txt", []byte("hello")); !result.OK {
		t.Fatalf("unexpected write error %v", result.Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := []FileErrorCode{
		env.TryReadTextFileContext(ctx, "file.txt").Error.Code,
		env.TryReadTextLinesContext(ctx, "file.txt", 0).Error.Code,
		env.TryReadBinaryFileContext(ctx, "file.txt").Error.Code,
		env.TryWriteFileContext(ctx, "other.txt", []byte("hello")).Error.Code,
		env.TryListDirContext(ctx, ".").Error.Code,
	}
	for _, code := range results {
		if code != FileErrorAborted {
			t.Fatalf("expected aborted file error, got %q", code)
		}
	}
}

func TestTruncationHelpers(t *testing.T) {
	head := TruncateHead("one\ntwo\nthree", TruncationOptions{MaxLines: 2, MaxBytes: 100})
	if !head.Truncated || head.TruncatedBy != "lines" || head.Content != "one\ntwo" {
		t.Fatalf("unexpected head %#v", head)
	}
	tail := TruncateTail("one\ntwo\nthree", TruncationOptions{MaxLines: 2, MaxBytes: 100})
	if !tail.Truncated || tail.TruncatedBy != "lines" || tail.Content != "two\nthree" {
		t.Fatalf("unexpected tail %#v", tail)
	}
	line, truncated := TruncateLine(strings.Repeat("x", 10), 4)
	if !truncated || line != "xxxx... [truncated]" {
		t.Fatalf("unexpected line %q %v", line, truncated)
	}
	if FormatSize(1536) != "1.5KB" {
		t.Fatalf("unexpected size")
	}
}

func TestLocalExecutionEnvRemove(t *testing.T) {
	env := NewLocalExecutionEnv(t.TempDir())
	path := env.AbsolutePath("dir")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := env.Remove("dir", true, false); err != nil {
		t.Fatal(err)
	}
}
