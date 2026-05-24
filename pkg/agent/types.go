package agent

import (
	"context"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type ToolExecutionMode string
type QueueMode string
type ThinkingLevel string

const (
	ToolExecutionSequential ToolExecutionMode = "sequential"
	ToolExecutionParallel   ToolExecutionMode = "parallel"

	QueueModeAll        QueueMode = "all"
	QueueModeOneAtATime QueueMode = "one-at-a-time"

	ThinkingLevelOff     ThinkingLevel = "off"
	ThinkingLevelMinimal ThinkingLevel = "minimal"
	ThinkingLevelLow     ThinkingLevel = "low"
	ThinkingLevelMedium  ThinkingLevel = "medium"
	ThinkingLevelHigh    ThinkingLevel = "high"
	ThinkingLevelXHigh   ThinkingLevel = "xhigh"
)

type AgentMessage = ai.Message
type AgentToolCall = ai.ToolCall

type StreamFn func(context.Context, ai.Model, ai.Context, ai.SimpleStreamOptions) *ai.AssistantMessageEventStream

type AgentToolResult[T any] struct {
	Content   []ai.ContentBlock `json:"content"`
	Details   T                 `json:"details"`
	Terminate bool              `json:"terminate,omitempty"`
}

type AgentToolUpdateCallback[T any] func(AgentToolResult[T])

type AgentTool[T any] struct {
	ai.Tool
	Label            string
	PrepareArguments func(args any) (T, error)
	Execute          func(ctx context.Context, toolCallID string, params any, onUpdate AgentToolUpdateCallback[T]) (AgentToolResult[any], error)
	ExecutionMode    ToolExecutionMode
}

type AgentContext struct {
	SystemPrompt string           `json:"systemPrompt,omitempty"`
	Messages     []AgentMessage   `json:"messages"`
	Tools        []AgentTool[any] `json:"tools,omitempty"`
}

type AgentState struct {
	SystemPrompt     string
	Model            ai.Model
	ThinkingLevel    ThinkingLevel
	Tools            []AgentTool[any]
	Messages         []AgentMessage
	IsStreaming      bool
	StreamingMessage *AgentMessage
	PendingToolCalls map[string]bool
	ErrorMessage     string
}

type BeforeToolCallResult struct {
	Block  bool
	Reason string
}

type AfterToolCallResult struct {
	Content   []ai.ContentBlock
	Details   any
	IsError   *bool
	Terminate *bool
}

type BeforeToolCallContext struct {
	AssistantMessage ai.Message
	ToolCall         AgentToolCall
	Args             any
	Context          AgentContext
}

type AfterToolCallContext struct {
	AssistantMessage ai.Message
	ToolCall         AgentToolCall
	Args             any
	Result           AgentToolResult[any]
	IsError          bool
	Context          AgentContext
}

type AgentLoopTurnUpdate struct {
	Context       *AgentContext
	Model         *ai.Model
	ThinkingLevel *ThinkingLevel
}

type PrepareNextTurnContext = ShouldStopAfterTurnContext

type AgentLoopConfig struct {
	Model               ai.Model
	Reasoning           *ThinkingLevel
	SessionID           string
	Transport           ai.Transport
	CacheRetention      ai.CacheRetention
	ThinkingBudgets     *ai.ThinkingBudgets
	MaxRetryDelayMs     *int
	OnPayload           func(payload any, model ai.Model) (any, bool, error)
	OnResponse          func(response ai.ProviderResponse, model ai.Model) error
	ToolExecution       ToolExecutionMode
	ConvertToLlm        func([]AgentMessage) ([]ai.Message, error)
	TransformContext    func(context.Context, []AgentMessage) ([]AgentMessage, error)
	GetAPIKey           func(context.Context, ai.Provider) (string, error)
	GetSteeringMessages func(context.Context) ([]AgentMessage, error)
	GetFollowUpMessages func(context.Context) ([]AgentMessage, error)
	BeforeToolCall      func(context.Context, BeforeToolCallContext) (*BeforeToolCallResult, error)
	AfterToolCall       func(context.Context, AfterToolCallContext) (*AfterToolCallResult, error)
	PrepareNextTurn     func(context.Context, PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)
	ShouldStopAfterTurn func(context.Context, ShouldStopAfterTurnContext) (bool, error)
}

type ShouldStopAfterTurnContext struct {
	Message     ai.Message
	ToolResults []ai.Message
	Context     AgentContext
	NewMessages []AgentMessage
}

type AgentEvent struct {
	Type                  string
	Messages              []AgentMessage
	Message               *AgentMessage
	AssistantMessageEvent *ai.AssistantMessageEvent
	ToolResults           []ai.Message
	ToolCallID            string
	ToolName              string
	Args                  any
	PartialResult         any
	Result                any
	IsError               bool
}
