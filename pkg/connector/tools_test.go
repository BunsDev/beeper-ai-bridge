package connector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
)

func TestWorkspaceToolsReadListAndWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := workspaceTools(harness.NewLocalExecutionEnv(dir))
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	readResult, err := tools[0].Execute(ctx, "call_read", map[string]any{"path": "input.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if readResult.Content[0].Text != "hello" {
		t.Fatalf("unexpected read result %#v", readResult)
	}
	listResult, err := tools[1].Execute(ctx, "call_list", map[string]any{"path": "."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if listResult.Content[0].Text == "" {
		t.Fatalf("expected list result text")
	}
	writeResult, err := tools[2].Execute(ctx, "call_write", map[string]any{"path": "output.txt", "content": "ok"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if writeResult.Content[0].Text != "written" {
		t.Fatalf("unexpected write result %#v", writeResult)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "ok" {
		t.Fatalf("unexpected written content %q", raw)
	}
	if _, err := tools[0].Execute(ctx, "call_escape", map[string]any{"path": "../outside.txt"}, nil); err == nil {
		t.Fatalf("expected workspace path escape to fail")
	}
}

func TestExecutionEnvRequiresConfiguredWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	client := &Client{Main: &Connector{Config: Config{
		RoomStateEventType: "com.beeper.ai.room_config",
		Tools:              ToolsConfig{Enabled: true, WorkspaceRoots: []string{root}},
	}}}
	env, err := client.executionEnv(RoomConfig{ToolsEnabled: true, Cwd: filepath.Join(root, "child")})
	if err != nil {
		t.Fatal(err)
	}
	if env == nil {
		t.Fatalf("expected execution env")
	}
	_, err = client.executionEnv(RoomConfig{ToolsEnabled: true, Cwd: filepath.Join(t.TempDir(), "outside")})
	if err == nil || !strings.Contains(err.Error(), "outside configured workspace roots") {
		t.Fatalf("expected outside root error, got %v", err)
	}
	_, err = client.executionEnv(RoomConfig{ToolsEnabled: true})
	if err == nil || !strings.Contains(err.Error(), "require cwd") {
		t.Fatalf("expected missing cwd error, got %v", err)
	}
}
