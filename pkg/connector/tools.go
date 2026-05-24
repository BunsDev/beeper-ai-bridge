package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	"github.com/beeper/ai-bridge/pkg/agent/harness"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func workspaceTools(env *harness.LocalExecutionEnv) []agent.AgentTool[any] {
	return []agent.AgentTool[any]{
		{
			Tool: ai.Tool{
				Name:        "read_text_file",
				Description: "Read a UTF-8 text file from the configured workspace.",
				Parameters: map[string]any{
					"type":     "object",
					"required": []string{"path"},
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
				},
			},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
				path, err := workspacePath(env, params, "path")
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				content, err := env.ReadTextFile(path)
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				return textToolResult(content), nil
			},
		},
		{
			Tool: ai.Tool{
				Name:        "list_directory",
				Description: "List files in a directory from the configured workspace.",
				Parameters: map[string]any{
					"type":     "object",
					"required": []string{"path"},
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
				},
			},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
				path, err := workspacePath(env, params, "path")
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				entries, err := env.ListDir(path)
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				raw, err := json.Marshal(entries)
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				return textToolResult(string(raw)), nil
			},
		},
		{
			Tool: ai.Tool{
				Name:        "write_text_file",
				Description: "Write a UTF-8 text file in the configured workspace.",
				Parameters: map[string]any{
					"type":     "object",
					"required": []string{"path", "content"},
					"properties": map[string]any{
						"path":    map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
				},
			},
			Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
				path, err := workspacePath(env, params, "path")
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				content, err := stringParam(params, "content")
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				if err := env.WriteFile(path, []byte(content)); err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				return textToolResult("written"), nil
			},
		},
	}
}

func textToolResult(text string) agent.AgentToolResult[any] {
	return agent.AgentToolResult[any]{
		Content: []ai.ContentBlock{{Type: "text", Text: text}},
	}
}

func stringParam(params any, key string) (string, error) {
	values, ok := params.(map[string]any)
	if !ok {
		return "", fmt.Errorf("tool parameters must be an object")
	}
	value, ok := values[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("missing string parameter %s", key)
	}
	return value, nil
}

func workspacePath(env *harness.LocalExecutionEnv, params any, key string) (string, error) {
	path, err := stringParam(params, key)
	if err != nil {
		return "", err
	}
	root := filepath.Clean(env.Cwd)
	absolute := env.AbsolutePath(path)
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("path %s is outside configured workspace", path)
	}
	return relative, nil
}
