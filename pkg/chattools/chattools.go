package chattools

import agent "github.com/beeper/ai-bridge/pkg/agent"

func Tools(info SessionInfo, fetch FetchOptions, search SearchOptions) []agent.AgentTool[any] {
	tools := []agent.AgentTool[any]{
		GetSessionTool(info),
		FetchTool(fetch),
	}
	if search.Enabled {
		tools = append(tools, WebSearchTool(search))
	}
	return tools
}
