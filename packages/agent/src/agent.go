package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

type AgentOptions struct {
	InitialState     *AgentState
	ConvertToLlm     func([]AgentMessage) ([]ai.Message, error)
	TransformContext func(context.Context, []AgentMessage) ([]AgentMessage, error)
	StreamFn         StreamFn
	GetAPIKey        func(context.Context, ai.Provider) (string, error)
	OnPayload        func(payload any, model ai.Model) (any, bool, error)
	OnResponse       func(response ai.ProviderResponse, model ai.Model) error
	BeforeToolCall   func(context.Context, BeforeToolCallContext) (*BeforeToolCallResult, error)
	AfterToolCall    func(context.Context, AfterToolCallContext) (*AfterToolCallResult, error)
	PrepareNextTurn  func(context.Context, PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)
	SteeringMode     QueueMode
	FollowUpMode     QueueMode
	SessionID        string
	ThinkingBudgets  *ai.ThinkingBudgets
	Transport        ai.Transport
	MaxRetryDelayMs  *int
	ToolExecution    ToolExecutionMode
}

type Agent struct {
	State            AgentState
	ConvertToLlm     func([]AgentMessage) ([]ai.Message, error)
	TransformContext func(context.Context, []AgentMessage) ([]AgentMessage, error)
	StreamFn         StreamFn
	GetAPIKey        func(context.Context, ai.Provider) (string, error)
	OnPayload        func(payload any, model ai.Model) (any, bool, error)
	OnResponse       func(response ai.ProviderResponse, model ai.Model) error
	BeforeToolCall   func(context.Context, BeforeToolCallContext) (*BeforeToolCallResult, error)
	AfterToolCall    func(context.Context, AfterToolCallContext) (*AfterToolCallResult, error)
	PrepareNextTurn  func(context.Context, PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)
	SessionID        string
	ThinkingBudgets  *ai.ThinkingBudgets
	Transport        ai.Transport
	MaxRetryDelayMs  *int
	ToolExecution    ToolExecutionMode
	listeners        []func(context.Context, AgentEvent) error
	nextListenerID   int
	listenerIDs      []int
	steeringQueue    pendingMessageQueue
	followUpQueue    pendingMessageQueue
	runMu            sync.Mutex
	nextRunID        int
	activeRunID      int
	cancelRun        context.CancelFunc
	runDone          chan struct{}
}

func NewAgent(options AgentOptions) *Agent {
	state := AgentState{ThinkingLevel: ThinkingLevelOff, PendingToolCalls: map[string]bool{}}
	if options.InitialState != nil {
		state = *options.InitialState
	}
	if state.PendingToolCalls == nil {
		state.PendingToolCalls = map[string]bool{}
	}
	transport := options.Transport
	if transport == "" {
		transport = ai.TransportAuto
	}
	toolExecution := options.ToolExecution
	if toolExecution == "" {
		toolExecution = ToolExecutionParallel
	}
	steeringMode := options.SteeringMode
	if steeringMode == "" {
		steeringMode = QueueModeOneAtATime
	}
	followUpMode := options.FollowUpMode
	if followUpMode == "" {
		followUpMode = QueueModeOneAtATime
	}
	convertToLlm := options.ConvertToLlm
	if convertToLlm == nil {
		convertToLlm = DefaultConvertToLlm
	}
	streamFn := options.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	return &Agent{State: state, ConvertToLlm: convertToLlm, TransformContext: options.TransformContext, StreamFn: streamFn, GetAPIKey: options.GetAPIKey, OnPayload: options.OnPayload, OnResponse: options.OnResponse, BeforeToolCall: options.BeforeToolCall, AfterToolCall: options.AfterToolCall, PrepareNextTurn: options.PrepareNextTurn, SessionID: options.SessionID, ThinkingBudgets: options.ThinkingBudgets, Transport: transport, MaxRetryDelayMs: options.MaxRetryDelayMs, ToolExecution: toolExecution, steeringQueue: pendingMessageQueue{mode: steeringMode}, followUpQueue: pendingMessageQueue{mode: followUpMode}}
}

func DefaultConvertToLlm(messages []AgentMessage) ([]ai.Message, error) {
	out := make([]ai.Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "user", "assistant", "toolResult":
			out = append(out, ai.Message(message))
		}
	}
	return out, nil
}

func (a *Agent) Subscribe(listener func(context.Context, AgentEvent) error) func() {
	a.nextListenerID++
	id := a.nextListenerID
	a.listeners = append(a.listeners, listener)
	a.listenerIDs = append(a.listenerIDs, id)
	return func() {
		for index, candidateID := range a.listenerIDs {
			if candidateID == id {
				a.listeners = append(a.listeners[:index], a.listeners[index+1:]...)
				a.listenerIDs = append(a.listenerIDs[:index], a.listenerIDs[index+1:]...)
				return
			}
		}
	}
}

func (a *Agent) Prompt(ctx context.Context, input string, images ...ai.ContentBlock) error {
	content := []ai.ContentBlock{{Type: "text", Text: input}}
	content = append(content, images...)
	message := AgentMessage{Role: "user", Content: content, Timestamp: time.Now().UnixMilli()}
	return a.runPromptMessages(ctx, []AgentMessage{message})
}

func (a *Agent) PromptMessage(ctx context.Context, message AgentMessage) error {
	return a.runPromptMessages(ctx, []AgentMessage{message})
}

func (a *Agent) PromptMessages(ctx context.Context, messages []AgentMessage) error {
	return a.runPromptMessages(ctx, messages)
}

func (a *Agent) Continue(ctx context.Context) error {
	if a.State.IsStreaming {
		return errors.New("Agent is already processing. Wait for completion before continuing.")
	}
	lastMessage, ok := lastAgentMessage(a.State.Messages)
	if !ok {
		return errors.New("No messages to continue from")
	}
	if lastMessage.Role == "assistant" {
		if steering := a.steeringQueue.drain(); len(steering) > 0 {
			return a.runPromptMessagesWithOptions(ctx, steering, runPromptOptions{SkipInitialSteeringPoll: true})
		}
		if followUps := a.followUpQueue.drain(); len(followUps) > 0 {
			return a.runPromptMessages(ctx, followUps)
		}
		return errors.New("Cannot continue from message role: assistant")
	}
	return a.runContinuation(ctx)
}

func (a *Agent) Abort() {
	a.runMu.Lock()
	cancel := a.cancelRun
	a.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Agent) WaitForIdle(ctx context.Context) error {
	a.runMu.Lock()
	done := a.runDone
	a.runMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) GetSteeringMode() QueueMode {
	return a.steeringQueue.mode
}

func (a *Agent) SetSteeringMode(mode QueueMode) {
	if mode == "" {
		mode = QueueModeOneAtATime
	}
	a.steeringQueue.mode = mode
}

func (a *Agent) GetFollowUpMode() QueueMode {
	return a.followUpQueue.mode
}

func (a *Agent) SetFollowUpMode(mode QueueMode) {
	if mode == "" {
		mode = QueueModeOneAtATime
	}
	a.followUpQueue.mode = mode
}

func (a *Agent) runPromptMessages(ctx context.Context, messages []AgentMessage) error {
	return a.runPromptMessagesWithOptions(ctx, messages, runPromptOptions{})
}

type runPromptOptions struct {
	SkipInitialSteeringPoll bool
}

func (a *Agent) runPromptMessagesWithOptions(ctx context.Context, messages []AgentMessage, options runPromptOptions) error {
	if a.State.IsStreaming {
		return errors.New("Agent is already processing a prompt. Use steer() or followUp() to queue messages, or wait for completion.")
	}
	a.State.IsStreaming = true
	runCtx, finish := a.startRun(ctx)
	defer func() {
		a.finishRunState()
		finish()
	}()
	_, err := RunAgentLoop(runCtx, messages, a.contextSnapshot(), a.loopConfigWithOptions(options), a.processEvent, a.StreamFn)
	if err != nil {
		return a.handleRunFailure(runCtx, err)
	}
	return nil
}

func (a *Agent) runContinuation(ctx context.Context) error {
	a.State.IsStreaming = true
	runCtx, finish := a.startRun(ctx)
	defer func() {
		a.finishRunState()
		finish()
	}()
	_, err := RunAgentLoopContinue(runCtx, a.contextSnapshot(), a.loopConfig(), a.processEvent, a.StreamFn)
	if err != nil {
		return a.handleRunFailure(runCtx, err)
	}
	return nil
}

func (a *Agent) Reset() {
	a.State.Messages = nil
	a.State.IsStreaming = false
	a.State.StreamingMessage = nil
	a.State.PendingToolCalls = map[string]bool{}
	a.State.ErrorMessage = ""
	a.ClearAllQueues()
}

func (a *Agent) finishRunState() {
	a.State.IsStreaming = false
	a.State.StreamingMessage = nil
	a.State.PendingToolCalls = map[string]bool{}
}

func (a *Agent) Steer(message AgentMessage) {
	a.steeringQueue.enqueue(message)
}

func (a *Agent) FollowUp(message AgentMessage) {
	a.followUpQueue.enqueue(message)
}

func (a *Agent) ClearSteeringQueue() {
	a.steeringQueue.clear()
}

func (a *Agent) ClearFollowUpQueue() {
	a.followUpQueue.clear()
}

func (a *Agent) ClearAllQueues() {
	a.ClearSteeringQueue()
	a.ClearFollowUpQueue()
}

func (a *Agent) HasQueuedMessages() bool {
	return a.steeringQueue.hasItems() || a.followUpQueue.hasItems()
}

func (a *Agent) startRun(ctx context.Context) (context.Context, func()) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.runMu.Lock()
	a.nextRunID++
	id := a.nextRunID
	a.activeRunID = id
	a.cancelRun = cancel
	a.runDone = done
	a.runMu.Unlock()
	finish := func() {
		a.runMu.Lock()
		if a.activeRunID == id {
			a.cancelRun = nil
			a.runDone = nil
			a.activeRunID = 0
		}
		close(done)
		a.runMu.Unlock()
	}
	return runCtx, finish
}

func (a *Agent) contextSnapshot() AgentContext {
	return AgentContext{SystemPrompt: a.State.SystemPrompt, Messages: append([]AgentMessage{}, a.State.Messages...), Tools: append([]AgentTool[any]{}, a.State.Tools...)}
}

func (a *Agent) loopConfig() AgentLoopConfig {
	return a.loopConfigWithOptions(runPromptOptions{})
}

func (a *Agent) loopConfigWithOptions(options runPromptOptions) AgentLoopConfig {
	var reasoning *ThinkingLevel
	if a.State.ThinkingLevel != ThinkingLevelOff {
		reasoning = &a.State.ThinkingLevel
	}
	skipInitialSteeringPoll := options.SkipInitialSteeringPoll
	return AgentLoopConfig{Model: a.State.Model, Reasoning: reasoning, SessionID: a.SessionID, Transport: a.Transport, ThinkingBudgets: a.ThinkingBudgets, MaxRetryDelayMs: a.MaxRetryDelayMs, OnPayload: a.OnPayload, OnResponse: a.OnResponse, ToolExecution: a.ToolExecution, ConvertToLlm: a.ConvertToLlm, TransformContext: a.TransformContext, GetAPIKey: a.GetAPIKey, BeforeToolCall: a.BeforeToolCall, AfterToolCall: a.AfterToolCall, PrepareNextTurn: a.PrepareNextTurn, GetSteeringMessages: func(context.Context) ([]AgentMessage, error) {
		if skipInitialSteeringPoll {
			skipInitialSteeringPoll = false
			return nil, nil
		}
		return a.steeringQueue.drain(), nil
	}, GetFollowUpMessages: func(context.Context) ([]AgentMessage, error) {
		return a.followUpQueue.drain(), nil
	}}
}

func (a *Agent) processEvent(ctx context.Context, event AgentEvent) error {
	switch event.Type {
	case "message_start", "message_update":
		a.State.StreamingMessage = event.Message
	case "message_end":
		a.State.StreamingMessage = nil
		if event.Message != nil {
			a.State.Messages = append(a.State.Messages, *event.Message)
		}
	case "tool_execution_start":
		a.State.PendingToolCalls[event.ToolCallID] = true
	case "tool_execution_end":
		delete(a.State.PendingToolCalls, event.ToolCallID)
	case "turn_end":
		if event.Message != nil && event.Message.ErrorMessage != "" {
			a.State.ErrorMessage = event.Message.ErrorMessage
		}
	}
	for _, listener := range a.listeners {
		if listener != nil {
			if err := listener(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Agent) handleRunFailure(ctx context.Context, err error) error {
	stopReason := ai.StopReasonError
	if errors.Is(ctx.Err(), context.Canceled) {
		stopReason = ai.StopReasonAborted
	}
	message := AgentMessage{
		Role:         "assistant",
		Content:      []ai.ContentBlock{{Type: "text", Text: ""}},
		API:          a.State.Model.API,
		Provider:     a.State.Model.Provider,
		Model:        a.State.Model.ID,
		Usage:        ai.EmptyUsage(),
		StopReason:   stopReason,
		ErrorMessage: err.Error(),
		Timestamp:    time.Now().UnixMilli(),
	}
	if eventErr := a.processEvent(ctx, AgentEvent{Type: "message_start", Message: &message}); eventErr != nil {
		return eventErr
	}
	if eventErr := a.processEvent(ctx, AgentEvent{Type: "message_end", Message: &message}); eventErr != nil {
		return eventErr
	}
	if eventErr := a.processEvent(ctx, AgentEvent{Type: "turn_end", Message: &message, ToolResults: []ai.Message{}}); eventErr != nil {
		return eventErr
	}
	return a.processEvent(ctx, AgentEvent{Type: "agent_end", Messages: []AgentMessage{message}})
}

type pendingMessageQueue struct {
	messages []AgentMessage
	mode     QueueMode
}

func (q *pendingMessageQueue) enqueue(message AgentMessage) {
	q.messages = append(q.messages, message)
}

func (q *pendingMessageQueue) hasItems() bool {
	return len(q.messages) > 0
}

func (q *pendingMessageQueue) drain() []AgentMessage {
	if len(q.messages) == 0 {
		return nil
	}
	if q.mode == QueueModeAll {
		drained := append([]AgentMessage{}, q.messages...)
		q.messages = nil
		return drained
	}
	first := q.messages[0]
	q.messages = q.messages[1:]
	return []AgentMessage{first}
}

func (q *pendingMessageQueue) clear() {
	q.messages = nil
}

func lastAgentMessage(messages []AgentMessage) (AgentMessage, bool) {
	if len(messages) == 0 {
		return AgentMessage{}, false
	}
	return messages[len(messages)-1], true
}
