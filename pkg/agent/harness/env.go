package harness

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type FileKind string

const (
	FileKindFile      FileKind = "file"
	FileKindDirectory FileKind = "directory"
	FileKindSymlink   FileKind = "symlink"
)

type FileInfo struct {
	Name    string
	Path    string
	Kind    FileKind
	Size    int64
	MtimeMs int64
}

type ExecOptions struct {
	Cwd      string
	Env      map[string]string
	Timeout  time.Duration
	OnStdout func(string)
	OnStderr func(string)
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type LocalExecutionEnv struct {
	Cwd       string
	ShellPath string
	ShellEnv  map[string]string
}

func NewLocalExecutionEnv(cwd string) *LocalExecutionEnv {
	return &LocalExecutionEnv{Cwd: cwd}
}

func (e *LocalExecutionEnv) AbsolutePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(e.Cwd, path)
}

func (e *LocalExecutionEnv) JoinPath(parts ...string) string {
	return filepath.Join(parts...)
}

func (e *LocalExecutionEnv) Exec(ctx context.Context, command string, options ExecOptions) (ExecResult, error) {
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	shell, args, err := e.shellConfig()
	if err != nil {
		return ExecResult{}, err
	}
	cmd := exec.CommandContext(ctx, shell, append(args, command)...)
	if options.Cwd != "" {
		cmd.Dir = e.AbsolutePath(options.Cwd)
	} else {
		cmd.Dir = e.Cwd
	}
	cmd.Env = mergeEnv(os.Environ(), e.ShellEnv, options.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = writerFunc(func(p []byte) (int, error) {
		stdout.Write(p)
		if options.OnStdout != nil {
			options.OnStdout(string(p))
		}
		return len(p), nil
	})
	cmd.Stderr = writerFunc(func(p []byte) (int, error) {
		stderr.Write(p)
		if options.OnStderr != nil {
			options.OnStderr(string(p))
		}
		return len(p), nil
	})
	if err := cmd.Start(); err != nil {
		return ExecResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		killProcessGroup(cmd.Process)
		waitErr = <-done
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, ctx.Err()
		}
		return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, ctx.Err()
	}
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecResult{}, waitErr
		}
	}
	return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}, nil
}

func (e *LocalExecutionEnv) ReadTextFile(path string) (string, error) {
	raw, err := os.ReadFile(e.AbsolutePath(path))
	return string(raw), err
}

func (e *LocalExecutionEnv) ReadTextLines(path string, maxLines int) ([]string, error) {
	file, err := os.Open(e.AbsolutePath(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if maxLines > 0 && len(lines) >= maxLines {
			break
		}
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func (e *LocalExecutionEnv) ReadBinaryFile(path string) ([]byte, error) {
	return os.ReadFile(e.AbsolutePath(path))
}

func (e *LocalExecutionEnv) WriteFile(path string, content []byte) error {
	resolved := e.AbsolutePath(path)
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolved, content, 0o644)
}

func (e *LocalExecutionEnv) AppendFile(path string, content []byte) error {
	resolved := e.AbsolutePath(path)
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(content)
	return err
}

func (e *LocalExecutionEnv) FileInfo(path string) (FileInfo, error) {
	resolved := e.AbsolutePath(path)
	info, err := os.Lstat(resolved)
	if err != nil {
		return FileInfo{}, err
	}
	kind := FileKindFile
	if info.IsDir() {
		kind = FileKindDirectory
	} else if info.Mode()&os.ModeSymlink != 0 {
		kind = FileKindSymlink
	}
	return FileInfo{Name: filepath.Base(resolved), Path: resolved, Kind: kind, Size: info.Size(), MtimeMs: info.ModTime().UnixMilli()}, nil
}

func (e *LocalExecutionEnv) ListDir(path string) ([]FileInfo, error) {
	resolved := e.AbsolutePath(path)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	infos := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := e.FileInfo(filepath.Join(resolved, entry.Name()))
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (e *LocalExecutionEnv) CanonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(e.AbsolutePath(path))
}

func (e *LocalExecutionEnv) Exists(path string) (bool, error) {
	_, err := e.FileInfo(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (e *LocalExecutionEnv) CreateDir(path string, recursive bool) error {
	if recursive {
		return os.MkdirAll(e.AbsolutePath(path), 0o755)
	}
	return os.Mkdir(e.AbsolutePath(path), 0o755)
}

func (e *LocalExecutionEnv) Remove(path string, recursive bool, force bool) error {
	resolved := e.AbsolutePath(path)
	if recursive {
		return os.RemoveAll(resolved)
	}
	err := os.Remove(resolved)
	if force && os.IsNotExist(err) {
		return nil
	}
	return err
}

func (e *LocalExecutionEnv) CreateTempDir(prefix string) (string, error) {
	if prefix == "" {
		prefix = "tmp-"
	}
	return os.MkdirTemp("", prefix)
}

func (e *LocalExecutionEnv) CreateTempFile(prefix string, suffix string) (string, error) {
	file, err := os.CreateTemp("", prefix+"*"+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	return path, file.Close()
}

func (e *LocalExecutionEnv) Cleanup() error {
	return nil
}

func (e *LocalExecutionEnv) AbsolutePathResult(path string) Result[string, *FileError] {
	return Ok[string, *FileError](e.AbsolutePath(path))
}

func (e *LocalExecutionEnv) JoinPathResult(parts ...string) Result[string, *FileError] {
	return Ok[string, *FileError](e.JoinPath(parts...))
}

func (e *LocalExecutionEnv) TryExec(ctx context.Context, command string, options ExecOptions) Result[ExecResult, *ExecutionError] {
	result, err := e.Exec(ctx, command, options)
	if err != nil {
		return Err[ExecResult](toExecutionError(err))
	}
	return Ok[ExecResult, *ExecutionError](result)
}

func (e *LocalExecutionEnv) TryReadTextFile(path string) Result[string, *FileError] {
	text, err := e.ReadTextFile(path)
	if err != nil {
		return Err[string](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[string, *FileError](text)
}

func (e *LocalExecutionEnv) TryReadTextFileContext(ctx context.Context, path string) Result[string, *FileError] {
	if err := ctx.Err(); err != nil {
		return Err[string](toFileError(err, e.AbsolutePath(path)))
	}
	return e.TryReadTextFile(path)
}

func (e *LocalExecutionEnv) TryReadTextLines(path string, maxLines int) Result[[]string, *FileError] {
	lines, err := e.ReadTextLines(path, maxLines)
	if err != nil {
		return Err[[]string](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[[]string, *FileError](lines)
}

func (e *LocalExecutionEnv) TryReadTextLinesContext(ctx context.Context, path string, maxLines int) Result[[]string, *FileError] {
	if err := ctx.Err(); err != nil {
		return Err[[]string](toFileError(err, e.AbsolutePath(path)))
	}
	return e.TryReadTextLines(path, maxLines)
}

func (e *LocalExecutionEnv) TryReadBinaryFile(path string) Result[[]byte, *FileError] {
	content, err := e.ReadBinaryFile(path)
	if err != nil {
		return Err[[]byte](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[[]byte, *FileError](content)
}

func (e *LocalExecutionEnv) TryReadBinaryFileContext(ctx context.Context, path string) Result[[]byte, *FileError] {
	if err := ctx.Err(); err != nil {
		return Err[[]byte](toFileError(err, e.AbsolutePath(path)))
	}
	return e.TryReadBinaryFile(path)
}

func (e *LocalExecutionEnv) TryWriteFile(path string, content []byte) Result[struct{}, *FileError] {
	if err := e.WriteFile(path, content); err != nil {
		return Err[struct{}](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[struct{}, *FileError](struct{}{})
}

func (e *LocalExecutionEnv) TryWriteFileContext(ctx context.Context, path string, content []byte) Result[struct{}, *FileError] {
	if err := ctx.Err(); err != nil {
		return Err[struct{}](toFileError(err, e.AbsolutePath(path)))
	}
	return e.TryWriteFile(path, content)
}

func (e *LocalExecutionEnv) TryAppendFile(path string, content []byte) Result[struct{}, *FileError] {
	if err := e.AppendFile(path, content); err != nil {
		return Err[struct{}](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[struct{}, *FileError](struct{}{})
}

func (e *LocalExecutionEnv) TryFileInfo(path string) Result[FileInfo, *FileError] {
	info, err := e.FileInfo(path)
	if err != nil {
		return Err[FileInfo](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[FileInfo, *FileError](info)
}

func (e *LocalExecutionEnv) TryListDir(path string) Result[[]FileInfo, *FileError] {
	infos, err := e.ListDir(path)
	if err != nil {
		return Err[[]FileInfo](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[[]FileInfo, *FileError](infos)
}

func (e *LocalExecutionEnv) TryListDirContext(ctx context.Context, path string) Result[[]FileInfo, *FileError] {
	if err := ctx.Err(); err != nil {
		return Err[[]FileInfo](toFileError(err, e.AbsolutePath(path)))
	}
	return e.TryListDir(path)
}

func (e *LocalExecutionEnv) TryCanonicalPath(path string) Result[string, *FileError] {
	resolved, err := e.CanonicalPath(path)
	if err != nil {
		return Err[string](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[string, *FileError](resolved)
}

func (e *LocalExecutionEnv) TryExists(path string) Result[bool, *FileError] {
	exists, err := e.Exists(path)
	if err != nil {
		return Err[bool](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[bool, *FileError](exists)
}

func (e *LocalExecutionEnv) TryCreateDir(path string, recursive bool) Result[struct{}, *FileError] {
	if err := e.CreateDir(path, recursive); err != nil {
		return Err[struct{}](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[struct{}, *FileError](struct{}{})
}

func (e *LocalExecutionEnv) TryRemove(path string, recursive bool, force bool) Result[struct{}, *FileError] {
	if err := e.Remove(path, recursive, force); err != nil {
		return Err[struct{}](toFileError(err, e.AbsolutePath(path)))
	}
	return Ok[struct{}, *FileError](struct{}{})
}

func (e *LocalExecutionEnv) TryCreateTempDir(prefix string) Result[string, *FileError] {
	path, err := e.CreateTempDir(prefix)
	if err != nil {
		return Err[string](toFileError(err, ""))
	}
	return Ok[string, *FileError](path)
}

func (e *LocalExecutionEnv) TryCreateTempFile(prefix string, suffix string) Result[string, *FileError] {
	path, err := e.CreateTempFile(prefix, suffix)
	if err != nil {
		return Err[string](toFileError(err, ""))
	}
	return Ok[string, *FileError](path)
}

func (e *LocalExecutionEnv) shellConfig() (string, []string, error) {
	if e.ShellPath != "" {
		if _, err := os.Stat(e.ShellPath); err != nil {
			return "", nil, err
		}
		return e.ShellPath, []string{"-c"}, nil
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"-c"}, nil
	}
	return "sh", []string{"-c"}, nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func mergeEnv(base []string, envs ...map[string]string) []string {
	values := map[string]string{}
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	for _, env := range envs {
		for key, value := range env {
			values[key] = value
		}
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

func killProcessGroup(process *os.Process) {
	if process == nil {
		return
	}
	_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	_ = process.Kill()
}
