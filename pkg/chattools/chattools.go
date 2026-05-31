package chattools

import agent "github.com/beeper/ai-bridge/pkg/agent"

func Tools(info SessionInfo, fetch FetchOptions, search SearchOptions) []agent.AgentTool[any] {
	return ToolsWithOptions(info, fetch, search, SessionOptions{})
}

func ToolsWithOptions(info SessionInfo, fetch FetchOptions, search SearchOptions, sessionOptions SessionOptions) []agent.AgentTool[any] {
	tools := []agent.AgentTool[any]{
		GetSessionToolWithOptions(info, sessionOptions),
		FetchTool(fetch),
	}
	if search.Enabled {
		tools = append(tools, WebSearchTool(search))
	}
	return tools
}
