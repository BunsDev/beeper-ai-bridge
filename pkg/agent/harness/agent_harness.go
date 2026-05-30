package harness

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/rs/zerolog"
)

type AgentHarnessStreamOptions struct {
	Transport       ai.Transport
	TimeoutMs       *int
	MaxRetries      *int
	MaxRetryDelayMs *int
	Headers         map[string]string
	Metadata        map[string]any
	CacheRetention  ai.CacheRetention
}

type AgentHarnessOptions struct {
	Session               *session.Session
	Model                 ai.Model
	ThinkingLevel         agent.ThinkingLevel
	SystemPrompt          string
	SystemPromptFunc      func(context.Context, AgentHarnessSystemPromptContext) (string, error)
	StreamOptions         AgentHarnessStreamOptions
	Tools                 []agent.AgentTool[any]
	ActiveToolNames       []string
	SteeringMode          agent.QueueMode
	FollowUpMode          agent.QueueMode
	StreamFn              agent.StreamFn
	GetAPIKey             func(context.Context, ai.Provider) (string, error)
	GetAPIKeyAndHeaders   func(context.Context, ai.Model) (*AgentHarnessAuth, error)
	TransformContext      func(context.Context, []agent.AgentMessage) ([]agent.AgentMessage, error)
	GenerateSummary       func(context.Context, CompactionPreparation) (CompactResult, error)
	GenerateBranchSummary func(context.Context, BranchPreparation) (BranchSummaryResult, error)
}

type AgentHarnessAuth struct {
	APIKey  string
	Headers map[string]string
}

type AgentHarness struct {
	session               *session.Session
	phase                 string
	pendingSessionWrites  []pendingSessionWrite
	model                 ai.Model
	thinkingLevel         agent.ThinkingLevel
	systemPrompt          string
	systemPromptFunc      func(context.Context, AgentHarnessSystemPromptContext) (string, error)
	streamOptions         AgentHarnessStreamOptions
	tools                 map[string]agent.AgentTool[any]
	activeToolNames       []string
	steerQueue            []agent.AgentMessage
	steeringMode          agent.QueueMode
	followUpQueue         []agent.AgentMessage
	followUpMode          agent.QueueMode
	nextTurnQueue         []agent.AgentMessage
	streamFn              agent.StreamFn
	getAPIKey             func(context.Context, ai.Provider) (string, error)
	getAPIKeyAndHeaders   func(context.Context, ai.Model) (*AgentHarnessAuth, error)
	transformContext      func(context.Context, []agent.AgentMessage) ([]agent.AgentMessage, error)
	generateSummary       func(context.Context, CompactionPreparation) (CompactResult, error)
	generateBranchSummary func(context.Context, BranchPreparation) (BranchSummaryResult, error)
	subscribers           []func(context.Context, AgentHarnessEvent) error
	nextSubscriberID      int
	subscriberIDs         []int
	handlers              map[string][]func(context.Context, AgentHarnessEvent) (any, error)
	nextHandlerID         int
	handlerIDs            map[string][]int
	runMu                 sync.Mutex
	nextRunID             int
	activeRunID           int
	cancelRun             context.CancelFunc
	runDone               chan struct{}
	promptEntryIDs        []PromptEntryID
}

type AgentHarnessSystemPromptContext struct {
	Session       *session.Session
	Model         ai.Model
	ThinkingLevel agent.ThinkingLevel
	ActiveTools   []agent.AgentTool[any]
}

type pendingSessionWrite struct {
	Type          string
	Message       agent.AgentMessage
	Provider      ai.Provider
	ModelID       string
	ThinkingLevel agent.ThinkingLevel
}

type AgentHarnessEvent struct {
	Type                  string
	AgentEvent            *agent.AgentEvent
	Steer                 []agent.AgentMessage
	FollowUp              []agent.AgentMessage
	NextTurn              []agent.AgentMessage
	Message               *agent.AgentMessage
	Messages              []agent.AgentMessage
	SessionEntryID        string
	Model                 *ai.Model
	PreviousModel         *ai.Model
	ThinkingLevel         agent.ThinkingLevel
	PreviousLevel         agent.ThinkingLevel
	HadPendingMutations   bool
	NextTurnCount         int
	CompactionEntry       json.RawMessage
	BranchSummaryEntry    json.RawMessage
	OldLeafID             *string
	NewLeafID             *string
	FromHook              bool
	ClearedSteer          []agent.AgentMessage
	ClearedFollowUp       []agent.AgentMessage
	Prompt                string
	Attachments           []ai.ContentBlock
	SystemPrompt          string
	ToolCallID            string
	ToolName              string
	Input                 any
	Content               []ai.ContentBlock
	Details               any
	IsError               bool
	Payload               any
	ProviderResponse      ai.ProviderResponse
	StreamOptions         AgentHarnessStreamOptions
	SessionID             string
	CompactionPreparation *CompactionPreparation
	BranchPreparation     *TreePreparation
	BranchEntries         []json.RawMessage
	CustomInstructions    string
}

type CompactResult struct {
	Summary          string
	FirstKeptEntryID string
	TokensBefore     int
	Details          any
}

type NavigateTreeOptions struct {
	Summarize           bool
	CustomInstructions  string
	ReplaceInstructions bool
	Label               string
}

type NavigateTreeResult struct {
	Cancelled    bool
	EditorText   string
	SummaryEntry json.RawMessage
}

type BranchSummaryResult struct {
	Summary string
}

type AbortResult struct {
	ClearedSteer    []agent.AgentMessage
	ClearedFollowUp []agent.AgentMessage
}

type PromptResult struct {
	Message          agent.AgentMessage
	UserEntryID      string
	AssistantEntryID string
	EntryIDs         []PromptEntryID
}

type PromptEntryID struct {
	ID   string
	Role string
}

type BeforeAgentStartResult struct {
	Messages     []agent.AgentMessage
	SystemPrompt string
}

type ContextResult struct {
	Messages []agent.AgentMessage
}

type ToolCallResult struct {
	Block  bool
	Reason string
}

type ToolResultPatch struct {
	Content   []ai.ContentBlock
	Details   any
	IsError   *bool
	Terminate *bool
}

type BeforeProviderRequestResult struct {
	StreamOptions AgentHarnessStreamOptions
}

type BeforeProviderPayloadResult struct {
	Payload any
}

type SessionBeforeCompactResult struct {
	Cancel     bool
	Compaction *CompactResult
}

type TreePreparation struct {
	TargetID            string
	OldLeafID           *string
	CommonAncestorID    *string
	EntriesToSummarize  []json.RawMessage
	UserWantsSummary    bool
	CustomInstructions  string
	ReplaceInstructions bool
	Label               string
}

type SessionBeforeTreeResult struct {
	Cancel              bool
	Summary             *BranchSummaryResult
	CustomInstructions  string
	ReplaceInstructions bool
	Label               string
}

func NewAgentHarness(options AgentHarnessOptions) (*AgentHarness, error) {
	if options.Session == nil {
		return nil, errors.New("session is required")
	}
	tools := map[string]agent.AgentTool[any]{}
	for _, tool := range options.Tools {
		tools[tool.Name] = tool
	}
	activeToolNames := append([]string{}, options.ActiveToolNames...)
	if len(activeToolNames) == 0 {
		for _, tool := range options.Tools {
			activeToolNames = append(activeToolNames, tool.Name)
		}
	}
	streamFn := options.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	harness := &AgentHarness{
		session:               options.Session,
		phase:                 "idle",
		model:                 options.Model,
		thinkingLevel:         options.ThinkingLevel,
		systemPrompt:          options.SystemPrompt,
		systemPromptFunc:      options.SystemPromptFunc,
		streamOptions:         cloneHarnessStreamOptions(options.StreamOptions),
		tools:                 tools,
		activeToolNames:       activeToolNames,
		steeringMode:          options.SteeringMode,
		followUpMode:          options.FollowUpMode,
		streamFn:              streamFn,
		getAPIKey:             options.GetAPIKey,
		getAPIKeyAndHeaders:   options.GetAPIKeyAndHeaders,
		transformContext:      options.TransformContext,
		generateSummary:       options.GenerateSummary,
		generateBranchSummary: options.GenerateBranchSummary,
		handlers:              map[string][]func(context.Context, AgentHarnessEvent) (any, error){},
		handlerIDs:            map[string][]int{},
	}
	if harness.thinkingLevel == "" {
		harness.thinkingLevel = agent.ThinkingLevelOff
	}
	if harness.steeringMode == "" {
		harness.steeringMode = agent.QueueModeOneAtATime
	}
	if harness.followUpMode == "" {
		harness.followUpMode = agent.QueueModeOneAtATime
	}
	if err := harness.validateToolNames(activeToolNames); err != nil {
		return nil, err
	}
	return harness, nil
}

func (h *AgentHarness) Prompt(ctx context.Context, text string, attachments ...ai.ContentBlock) (agent.AgentMessage, error) {
	result, err := h.PromptWithResult(ctx, text, attachments...)
	return result.Message, err
}

func (h *AgentHarness) PromptWithResult(ctx context.Context, text string, attachments ...ai.ContentBlock) (PromptResult, error) {
	if h.phase != "idle" {
		return PromptResult{}, errors.New("AgentHarness is busy")
	}
	h.phase = "turn"
	h.promptEntryIDs = nil
	runCtx, finish := h.startRun(ctx)
	defer func() {
		h.phase = "idle"
		h.promptEntryIDs = nil
		finish()
	}()
	turnContext, err := h.createContext(runCtx)
	if err != nil {
		return PromptResult{}, err
	}
	messages := []agent.AgentMessage{createUserMessage(text, attachments)}
	if len(h.nextTurnQueue) > 0 {
		messages = append(append([]agent.AgentMessage{}, h.nextTurnQueue...), messages...)
		h.nextTurnQueue = nil
		if err := h.emit(runCtx, AgentHarnessEvent{Type: "queue_update", Steer: h.steerQueue, FollowUp: h.followUpQueue, NextTurn: h.nextTurnQueue}); err != nil {
			return PromptResult{}, err
		}
	}
	if before, err := h.emitHook(runCtx, AgentHarnessEvent{Type: "before_agent_start", Prompt: text, Attachments: attachments, SystemPrompt: turnContext.SystemPrompt}); err != nil {
		return PromptResult{}, err
	} else if result, ok := before.(BeforeAgentStartResult); ok {
		if len(result.Messages) > 0 {
			messages = append(messages, result.Messages...)
		}
		if result.SystemPrompt != "" {
			turnContext.SystemPrompt = result.SystemPrompt
		}
	}
	newMessages, err := agent.RunAgentLoop(runCtx, messages, turnContext, h.loopConfig(runCtx), h.handleAgentEvent, h.createStreamFn())
	if err != nil {
		if flushErr := h.flushPendingSessionWrites(runCtx); flushErr != nil {
			return PromptResult{}, flushErr
		}
		return PromptResult{}, err
	}
	if err := h.flushPendingSessionWrites(runCtx); err != nil {
		return PromptResult{}, err
	}
	result := PromptResult{EntryIDs: append([]PromptEntryID{}, h.promptEntryIDs...)}
	for _, entry := range result.EntryIDs {
		switch entry.Role {
		case "user":
			result.UserEntryID = entry.ID
		case "assistant":
			result.AssistantEntryID = entry.ID
		}
	}
	for i := len(newMessages) - 1; i >= 0; i-- {
		if newMessages[i].Role == "assistant" {
			result.Message = newMessages[i]
			return result, nil
		}
	}
	return PromptResult{}, errors.New("AgentHarness prompt completed without an assistant message")
}

func (h *AgentHarness) AppendMessage(ctx context.Context, message agent.AgentMessage) error {
	if h.phase == "idle" {
		_, err := h.session.AppendMessage(ctx, message)
		return err
	}
	h.pendingSessionWrites = append(h.pendingSessionWrites, pendingSessionWrite{Type: "message", Message: message})
	return nil
}

func (h *AgentHarness) Compact(ctx context.Context, customInstructions string) (CompactResult, error) {
	if h.phase != "idle" {
		return CompactResult{}, errors.New("compact() requires idle harness")
	}
	h.phase = "compaction"
	defer func() { h.phase = "idle" }()
	branch, err := h.session.GetBranch(ctx, nil)
	if err != nil {
		return CompactResult{}, err
	}
	preparation, ok, err := PrepareCompaction(branch, DefaultCompactionSettings)
	if err != nil {
		return CompactResult{}, err
	}
	if !ok || preparation == nil {
		return CompactResult{}, errors.New("Nothing to compact")
	}
	hookResult, err := h.emitHook(ctx, AgentHarnessEvent{Type: "session_before_compact", CompactionPreparation: preparation, BranchEntries: branch, CustomInstructions: customInstructions})
	if err != nil {
		return CompactResult{}, err
	}
	fromHook := false
	var result CompactResult
	if before, ok := hookResult.(SessionBeforeCompactResult); ok {
		if before.Cancel {
			return CompactResult{}, errors.New("Compaction cancelled")
		}
		if before.Compaction != nil {
			result = *before.Compaction
			fromHook = true
		}
	}
	if result.Summary == "" && h.generateSummary != nil {
		result, err = h.generateSummary(ctx, *preparation)
		if err != nil {
			return CompactResult{}, err
		}
	} else if result.Summary == "" && h.streamFn != nil {
		result, err = GenerateSummary(ctx, *preparation, h.summaryGenerationOptions(ctx, customInstructions))
		if err != nil {
			return CompactResult{}, err
		}
	} else if result.Summary == "" {
		result = CompactResult{Summary: "No summary generated", FirstKeptEntryID: preparation.FirstKeptEntryID, TokensBefore: preparation.TokensBefore}
	}
	if result.FirstKeptEntryID == "" {
		result.FirstKeptEntryID = preparation.FirstKeptEntryID
	}
	if result.TokensBefore == 0 {
		result.TokensBefore = preparation.TokensBefore
	}
	entryID, err := h.session.AppendCompaction(ctx, result.Summary, result.FirstKeptEntryID, result.TokensBefore, result.Details, &fromHook)
	if err != nil {
		return CompactResult{}, err
	}
	raw, _ := h.session.GetEntry(ctx, entryID)
	if err := h.emit(ctx, AgentHarnessEvent{Type: "session_compact", CompactionEntry: raw, FromHook: fromHook}); err != nil {
		return CompactResult{}, err
	}
	return result, nil
}

func (h *AgentHarness) NavigateTree(ctx context.Context, targetID string, options NavigateTreeOptions) (NavigateTreeResult, error) {
	if h.phase != "idle" {
		return NavigateTreeResult{}, errors.New("navigateTree() requires idle harness")
	}
	h.phase = "branch_summary"
	defer func() { h.phase = "idle" }()
	oldLeafID, err := h.session.GetLeafID(ctx)
	if err != nil {
		return NavigateTreeResult{}, err
	}
	if oldLeafID != nil && *oldLeafID == targetID {
		return NavigateTreeResult{Cancelled: false}, nil
	}
	targetRaw, err := h.session.GetEntry(ctx, targetID)
	if err != nil {
		return NavigateTreeResult{}, err
	}
	targetEntry, err := parseSessionEntry(targetRaw)
	if err != nil {
		return NavigateTreeResult{}, err
	}
	collected, err := CollectEntriesForBranchSummary(ctx, h.session, oldLeafID, targetID)
	if err != nil {
		return NavigateTreeResult{}, err
	}
	treePreparation := TreePreparation{TargetID: targetID, OldLeafID: oldLeafID, CommonAncestorID: collected.CommonAncestorID, EntriesToSummarize: collected.Entries, UserWantsSummary: options.Summarize, CustomInstructions: options.CustomInstructions, ReplaceInstructions: options.ReplaceInstructions, Label: options.Label}
	hookResult, err := h.emitHook(ctx, AgentHarnessEvent{Type: "session_before_tree", BranchPreparation: &treePreparation})
	if err != nil {
		return NavigateTreeResult{}, err
	}
	fromHook := false
	var summaryText string
	var summaryDetails any
	if before, ok := hookResult.(SessionBeforeTreeResult); ok {
		if before.Cancel {
			return NavigateTreeResult{Cancelled: true}, nil
		}
		if before.CustomInstructions != "" {
			options.CustomInstructions = before.CustomInstructions
		}
		if before.ReplaceInstructions {
			options.ReplaceInstructions = before.ReplaceInstructions
		}
		if before.Label != "" {
			options.Label = before.Label
		}
		if before.Summary != nil {
			summaryText = before.Summary.Summary
			fromHook = true
		}
	}
	if summaryText == "" && options.Summarize && len(collected.Entries) > 0 {
		preparation, err := PrepareBranchEntries(collected.Entries, branchSummaryTokenBudget(h.model))
		if err != nil {
			return NavigateTreeResult{}, err
		}
		var summary BranchSummaryResult
		if h.generateBranchSummary != nil {
			summary, err = h.generateBranchSummary(ctx, preparation)
			if err != nil {
				return NavigateTreeResult{}, err
			}
		} else if h.streamFn != nil {
			summaryOptions := h.summaryGenerationOptions(ctx, options.CustomInstructions)
			summaryOptions.ReplaceInstructions = options.ReplaceInstructions
			summary, err = GenerateBranchSummary(ctx, preparation, summaryOptions)
			if err != nil {
				return NavigateTreeResult{}, err
			}
		} else {
			summary = BranchSummaryResult{Summary: "No summary generated"}
		}
		summaryText = summary.Summary
	}
	newLeafID := &targetID
	editorText := ""
	if targetEntry.Type == "message" && targetEntry.Message.Role == "user" {
		newLeafID = targetEntry.ParentID
		editorText = textFromContent(targetEntry.Message.Content)
	} else if targetEntry.Type == "custom_message" {
		newLeafID = targetEntry.ParentID
		editorText = textFromContent(targetEntry.Content)
	}
	var summaryEntry json.RawMessage
	if summaryText != "" {
		summaryID, err := h.session.MoveTo(ctx, newLeafID, &session.MoveToSummary{Summary: summaryText, Details: summaryDetails, FromHook: &fromHook})
		if err != nil {
			return NavigateTreeResult{}, err
		}
		if summaryID != nil {
			summaryEntry, _ = h.session.GetEntry(ctx, *summaryID)
		}
	} else if err := h.session.GetStorage().SetLeafID(ctx, newLeafID); err != nil {
		return NavigateTreeResult{}, err
	}
	currentLeafID, _ := h.session.GetLeafID(ctx)
	if err := h.emit(ctx, AgentHarnessEvent{Type: "session_tree", OldLeafID: oldLeafID, NewLeafID: currentLeafID, BranchSummaryEntry: summaryEntry, FromHook: fromHook}); err != nil {
		return NavigateTreeResult{}, err
	}
	return NavigateTreeResult{Cancelled: false, EditorText: editorText, SummaryEntry: summaryEntry}, nil
}

func (h *AgentHarness) GetModel() ai.Model {
	return h.model
}

func (h *AgentHarness) SetModel(ctx context.Context, model ai.Model) error {
	previous := h.model
	if h.phase == "idle" {
		if _, err := h.session.AppendModelChange(ctx, string(model.Provider), model.ID); err != nil {
			return err
		}
	} else {
		h.pendingSessionWrites = append(h.pendingSessionWrites, pendingSessionWrite{Type: "model_change", Provider: model.Provider, ModelID: model.ID})
	}
	h.model = model
	return h.emit(ctx, AgentHarnessEvent{Type: "model_select", Model: &model, PreviousModel: &previous})
}

func (h *AgentHarness) GetThinkingLevel() agent.ThinkingLevel {
	return h.thinkingLevel
}

func (h *AgentHarness) SetThinkingLevel(ctx context.Context, level agent.ThinkingLevel) error {
	previous := h.thinkingLevel
	if h.phase == "idle" {
		if _, err := h.session.AppendThinkingLevelChange(ctx, string(level)); err != nil {
			return err
		}
	} else {
		h.pendingSessionWrites = append(h.pendingSessionWrites, pendingSessionWrite{Type: "thinking_level_change", ThinkingLevel: level})
	}
	h.thinkingLevel = level
	return h.emit(ctx, AgentHarnessEvent{Type: "thinking_level_select", ThinkingLevel: level, PreviousLevel: previous})
}

func (h *AgentHarness) GetSteeringMode() agent.QueueMode {
	return h.steeringMode
}

func (h *AgentHarness) SetSteeringMode(mode agent.QueueMode) {
	if mode == "" {
		mode = agent.QueueModeOneAtATime
	}
	h.steeringMode = mode
}

func (h *AgentHarness) GetFollowUpMode() agent.QueueMode {
	return h.followUpMode
}

func (h *AgentHarness) SetFollowUpMode(mode agent.QueueMode) {
	if mode == "" {
		mode = agent.QueueModeOneAtATime
	}
	h.followUpMode = mode
}

func (h *AgentHarness) NextTurn(ctx context.Context, text string, attachments ...ai.ContentBlock) error {
	h.nextTurnQueue = append(h.nextTurnQueue, createUserMessage(text, attachments))
	return h.emit(ctx, AgentHarnessEvent{Type: "queue_update", Steer: h.steerQueue, FollowUp: h.followUpQueue, NextTurn: h.nextTurnQueue})
}

func (h *AgentHarness) Steer(ctx context.Context, text string, attachments ...ai.ContentBlock) error {
	if h.phase == "idle" {
		return errors.New("Cannot steer while idle")
	}
	h.steerQueue = append(h.steerQueue, createUserMessage(text, attachments))
	return h.emit(ctx, AgentHarnessEvent{Type: "queue_update", Steer: h.steerQueue, FollowUp: h.followUpQueue, NextTurn: h.nextTurnQueue})
}

func (h *AgentHarness) FollowUp(ctx context.Context, text string, attachments ...ai.ContentBlock) error {
	if h.phase == "idle" {
		return errors.New("Cannot follow up while idle")
	}
	h.followUpQueue = append(h.followUpQueue, createUserMessage(text, attachments))
	return h.emit(ctx, AgentHarnessEvent{Type: "queue_update", Steer: h.steerQueue, FollowUp: h.followUpQueue, NextTurn: h.nextTurnQueue})
}

func (h *AgentHarness) GetStreamOptions() AgentHarnessStreamOptions {
	return cloneHarnessStreamOptions(h.streamOptions)
}

func (h *AgentHarness) SetStreamOptions(options AgentHarnessStreamOptions) {
	h.streamOptions = cloneHarnessStreamOptions(options)
}

func (h *AgentHarness) SetTools(tools []agent.AgentTool[any], activeToolNames []string) error {
	next := map[string]agent.AgentTool[any]{}
	for _, tool := range tools {
		next[tool.Name] = tool
	}
	if len(activeToolNames) == 0 {
		activeToolNames = h.activeToolNames
	}
	if err := validateToolNames(activeToolNames, next); err != nil {
		return err
	}
	h.tools = next
	h.activeToolNames = append([]string{}, activeToolNames...)
	return nil
}

func (h *AgentHarness) SetActiveTools(toolNames []string) error {
	if err := h.validateToolNames(toolNames); err != nil {
		return err
	}
	h.activeToolNames = append([]string{}, toolNames...)
	return nil
}

func (h *AgentHarness) Abort(ctx context.Context) (AbortResult, error) {
	clearedSteer := append([]agent.AgentMessage{}, h.steerQueue...)
	clearedFollowUp := append([]agent.AgentMessage{}, h.followUpQueue...)
	h.steerQueue = nil
	h.followUpQueue = nil
	h.runMu.Lock()
	cancel := h.cancelRun
	h.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	var emitErr error
	if err := h.emit(ctx, AgentHarnessEvent{Type: "queue_update", Steer: h.steerQueue, FollowUp: h.followUpQueue, NextTurn: h.nextTurnQueue}); err != nil {
		emitErr = err
	}
	if err := h.WaitForIdle(ctx); err != nil && emitErr == nil {
		emitErr = err
	}
	if err := h.emit(ctx, AgentHarnessEvent{Type: "abort", ClearedSteer: clearedSteer, ClearedFollowUp: clearedFollowUp}); err != nil && emitErr == nil {
		emitErr = err
	}
	return AbortResult{ClearedSteer: clearedSteer, ClearedFollowUp: clearedFollowUp}, emitErr
}

func (h *AgentHarness) WaitForIdle(ctx context.Context) error {
	h.runMu.Lock()
	done := h.runDone
	h.runMu.Unlock()
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

func (h *AgentHarness) Subscribe(listener func(context.Context, AgentHarnessEvent) error) func() {
	h.nextSubscriberID++
	id := h.nextSubscriberID
	h.subscribers = append(h.subscribers, listener)
	h.subscriberIDs = append(h.subscriberIDs, id)
	return func() {
		for i, candidateID := range h.subscriberIDs {
			if candidateID == id {
				h.subscribers = append(h.subscribers[:i], h.subscribers[i+1:]...)
				h.subscriberIDs = append(h.subscriberIDs[:i], h.subscriberIDs[i+1:]...)
				return
			}
		}
	}
}

func (h *AgentHarness) On(eventType string, handler func(context.Context, AgentHarnessEvent) (any, error)) func() {
	h.nextHandlerID++
	id := h.nextHandlerID
	h.handlers[eventType] = append(h.handlers[eventType], handler)
	h.handlerIDs[eventType] = append(h.handlerIDs[eventType], id)
	return func() {
		handlers := h.handlers[eventType]
		ids := h.handlerIDs[eventType]
		for i, candidateID := range ids {
			if candidateID == id {
				h.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
				h.handlerIDs[eventType] = append(ids[:i], ids[i+1:]...)
				return
			}
		}
	}
}

func (h *AgentHarness) createContext(ctx context.Context) (agent.AgentContext, error) {
	sessionContext, err := h.session.BuildContext(ctx)
	if err != nil {
		return agent.AgentContext{}, err
	}
	systemPrompt, err := h.systemPromptOrDefault(ctx)
	if err != nil {
		return agent.AgentContext{}, err
	}
	return agent.AgentContext{
		SystemPrompt: systemPrompt,
		Messages:     append([]agent.AgentMessage{}, sessionContext.Messages...),
		Tools:        h.activeTools(),
	}, nil
}

func (h *AgentHarness) loopConfig(ctx context.Context) agent.AgentLoopConfig {
	reasoning := h.thinkingLevel
	var reasoningPtr *agent.ThinkingLevel
	if reasoning != "" && reasoning != agent.ThinkingLevelOff {
		reasoningPtr = &reasoning
	}
	metadata, _ := h.session.GetMetadata(ctx)
	return agent.AgentLoopConfig{
		Model:               h.model,
		Reasoning:           reasoningPtr,
		SessionID:           metadata.ID,
		Transport:           h.streamOptions.Transport,
		CacheRetention:      h.streamOptions.CacheRetention,
		MaxRetryDelayMs:     h.streamOptions.MaxRetryDelayMs,
		ConvertToLlm:        func(messages []agent.AgentMessage) ([]ai.Message, error) { return ConvertToLlm(messages), nil },
		TransformContext:    h.transformContextWithHooks,
		GetAPIKey:           h.getAPIKey,
		GetSteeringMessages: h.drainSteering,
		GetFollowUpMessages: h.drainFollowUp,
		BeforeToolCall:      h.beforeToolCallHook,
		AfterToolCall:       h.afterToolCallHook,
		PrepareNextTurn:     h.prepareNextTurn,
	}
}

func (h *AgentHarness) createStreamFn() agent.StreamFn {
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		log := zerolog.Ctx(ctx).With().
			Str("action", "ai_agent_provider").
			Str("provider", string(model.Provider)).
			Str("model_id", model.ID).
			Logger()
		ctx = log.WithContext(ctx)
		snapshot := h.GetStreamOptions()
		snapshot.Transport = options.Transport
		snapshot.MaxRetryDelayMs = options.MaxRetryDelayMs
		snapshot.CacheRetention = options.CacheRetention
		metadata, err := h.session.GetMetadata(ctx)
		if err != nil {
			log.Err(err).Msg("Failed to load AI session metadata before provider request")
			return hookErrorStream(model, err)
		}
		log = log.With().Str("session_id", metadata.ID).Logger()
		ctx = log.WithContext(ctx)
		if h.getAPIKeyAndHeaders != nil {
			auth, err := h.getAPIKeyAndHeaders(ctx, model)
			if err != nil {
				log.Err(err).Msg("Failed to load AI provider auth")
				return hookErrorStream(model, err)
			}
			if auth != nil {
				if auth.APIKey != "" {
					options.APIKey = auth.APIKey
				}
				snapshot.Headers = mergeStringMaps(snapshot.Headers, auth.Headers)
			}
		}
		if result, err := h.emitHook(ctx, AgentHarnessEvent{Type: "before_provider_request", Model: &model, StreamOptions: snapshot, SessionID: metadata.ID}); err != nil {
			log.Err(err).Msg("AI provider request hook failed")
			return hookErrorStream(model, err)
		} else if patch, ok := result.(BeforeProviderRequestResult); ok {
			snapshot = mergeStreamOptions(snapshot, patch.StreamOptions)
		}
		log.Debug().
			Str("transport", string(snapshot.Transport)).
			Str("cache_retention", string(snapshot.CacheRetention)).
			Bool("has_headers", len(snapshot.Headers) > 0).
			Bool("has_metadata", len(snapshot.Metadata) > 0).
			Msg("Starting AI provider stream")
		options.Transport = snapshot.Transport
		options.MaxRetryDelayMs = snapshot.MaxRetryDelayMs
		options.CacheRetention = snapshot.CacheRetention
		options.TimeoutMs = snapshot.TimeoutMs
		options.MaxRetries = snapshot.MaxRetries
		options.Headers = cloneStringMap(snapshot.Headers)
		options.Metadata = cloneAnyMap(snapshot.Metadata)
		previousOnPayload := options.OnPayload
		options.OnPayload = func(payload any, payloadModel ai.Model) (any, bool, error) {
			changed := false
			if previousOnPayload != nil {
				next, ok, err := previousOnPayload(payload, payloadModel)
				if err != nil {
					return nil, false, err
				}
				if ok {
					payload = next
					changed = true
				}
			}
			result, err := h.emitHook(ctx, AgentHarnessEvent{Type: "before_provider_payload", Model: &payloadModel, Payload: payload})
			if err != nil {
				log.Err(err).Str("payload_model_id", payloadModel.ID).Str("payload_provider", string(payloadModel.Provider)).Msg("AI provider payload hook failed")
				return nil, false, err
			}
			if patch, ok := result.(BeforeProviderPayloadResult); ok {
				return patch.Payload, true, nil
			}
			return payload, changed, nil
		}
		previousOnResponse := options.OnResponse
		options.OnResponse = func(response ai.ProviderResponse, responseModel ai.Model) error {
			if previousOnResponse != nil {
				if err := previousOnResponse(response, responseModel); err != nil {
					log.Err(err).
						Str("response_model_id", responseModel.ID).
						Str("response_provider", string(responseModel.Provider)).
						Int("status_code", response.Status).
						Msg("AI provider response callback failed")
					return err
				}
			}
			log.Debug().
				Str("response_model_id", responseModel.ID).
				Str("response_provider", string(responseModel.Provider)).
				Int("status_code", response.Status).
				Msg("Received AI provider response")
			_, err := h.emitHook(ctx, AgentHarnessEvent{Type: "after_provider_response", Model: &responseModel, ProviderResponse: response})
			if err != nil {
				log.Err(err).
					Str("response_model_id", responseModel.ID).
					Str("response_provider", string(responseModel.Provider)).
					Int("status_code", response.Status).
					Msg("AI provider response hook failed")
			}
			return err
		}
		return h.streamFn(ctx, model, llmContext, options)
	}
}

func (h *AgentHarness) contextSnapshot(ctx context.Context, messages []agent.AgentMessage) ([]agent.AgentMessage, error) {
	if h.transformContext != nil {
		next, err := h.transformContext(ctx, messages)
		if err != nil {
			return nil, err
		}
		messages = next
	}
	return messages, nil
}

func (h *AgentHarness) transformContextWithHooks(ctx context.Context, messages []agent.AgentMessage) ([]agent.AgentMessage, error) {
	next, err := h.contextSnapshot(ctx, messages)
	if err != nil {
		return nil, err
	}
	result, err := h.emitHook(ctx, AgentHarnessEvent{Type: "context", Messages: append([]agent.AgentMessage{}, next...)})
	if err != nil {
		return nil, err
	}
	if contextResult, ok := result.(ContextResult); ok && contextResult.Messages != nil {
		return contextResult.Messages, nil
	}
	return next, nil
}

func (h *AgentHarness) beforeToolCallHook(ctx context.Context, callContext agent.BeforeToolCallContext) (*agent.BeforeToolCallResult, error) {
	result, err := h.emitHook(ctx, AgentHarnessEvent{Type: "tool_call", ToolCallID: callContext.ToolCall.ID, ToolName: callContext.ToolCall.Name, Input: callContext.Args})
	if err != nil {
		return nil, err
	}
	if toolResult, ok := result.(ToolCallResult); ok {
		return &agent.BeforeToolCallResult{Block: toolResult.Block, Reason: toolResult.Reason}, nil
	}
	return nil, nil
}

func (h *AgentHarness) afterToolCallHook(ctx context.Context, callContext agent.AfterToolCallContext) (*agent.AfterToolCallResult, error) {
	result, err := h.emitHook(ctx, AgentHarnessEvent{Type: "tool_result", ToolCallID: callContext.ToolCall.ID, ToolName: callContext.ToolCall.Name, Input: callContext.Args, Content: callContext.Result.Content, Details: callContext.Result.Details, IsError: callContext.IsError})
	if err != nil {
		return nil, err
	}
	if patch, ok := result.(ToolResultPatch); ok {
		return &agent.AfterToolCallResult{Content: patch.Content, Details: patch.Details, IsError: patch.IsError, Terminate: patch.Terminate}, nil
	}
	return nil, nil
}

func (h *AgentHarness) handleAgentEvent(ctx context.Context, event agent.AgentEvent) error {
	var sessionEntryID string
	if event.Type == "message_end" && event.Message != nil {
		entryID, err := h.session.AppendMessage(ctx, *event.Message)
		if err != nil {
			return err
		}
		sessionEntryID = entryID
		h.promptEntryIDs = append(h.promptEntryIDs, PromptEntryID{ID: entryID, Role: event.Message.Role})
	}
	if event.Type == "turn_end" {
		var eventErr error
		if err := h.emit(ctx, AgentHarnessEvent{Type: event.Type, AgentEvent: &event, Message: event.Message, Messages: event.Messages}); err != nil {
			eventErr = err
		}
		hadPendingMutations := len(h.pendingSessionWrites) > 0
		if err := h.flushPendingSessionWrites(ctx); err != nil {
			return err
		}
		if eventErr != nil {
			return eventErr
		}
		if err := h.emit(ctx, AgentHarnessEvent{Type: "save_point", HadPendingMutations: hadPendingMutations}); err != nil {
			return err
		}
		return nil
	}
	if event.Type == "agent_end" {
		if err := h.flushPendingSessionWrites(ctx); err != nil {
			return err
		}
		if err := h.emit(ctx, AgentHarnessEvent{Type: event.Type, AgentEvent: &event, Message: event.Message, Messages: event.Messages}); err != nil {
			return err
		}
		if err := h.emit(ctx, AgentHarnessEvent{Type: "settled", NextTurnCount: len(h.nextTurnQueue)}); err != nil {
			return err
		}
		return nil
	}
	if err := h.emit(ctx, AgentHarnessEvent{Type: event.Type, AgentEvent: &event, Message: event.Message, Messages: event.Messages, SessionEntryID: sessionEntryID}); err != nil {
		return err
	}
	return nil
}

func (h *AgentHarness) flushPendingSessionWrites(ctx context.Context) error {
	for len(h.pendingSessionWrites) > 0 {
		write := h.pendingSessionWrites[0]
		switch write.Type {
		case "message":
			if _, err := h.session.AppendMessage(ctx, write.Message); err != nil {
				return err
			}
		case "model_change":
			if _, err := h.session.AppendModelChange(ctx, string(write.Provider), write.ModelID); err != nil {
				return err
			}
		case "thinking_level_change":
			if _, err := h.session.AppendThinkingLevelChange(ctx, string(write.ThinkingLevel)); err != nil {
				return err
			}
		}
		h.pendingSessionWrites = h.pendingSessionWrites[1:]
	}
	return nil
}

func (h *AgentHarness) prepareNextTurn(ctx context.Context, next agent.PrepareNextTurnContext) (*agent.AgentLoopTurnUpdate, error) {
	if err := h.flushPendingSessionWrites(ctx); err != nil {
		return nil, err
	}
	nextContext, err := h.createContext(ctx)
	if err != nil {
		return nil, err
	}
	model := h.model
	thinkingLevel := h.thinkingLevel
	return &agent.AgentLoopTurnUpdate{Context: &nextContext, Model: &model, ThinkingLevel: &thinkingLevel}, nil
}

func (h *AgentHarness) drainSteering(ctx context.Context) ([]agent.AgentMessage, error) {
	return h.drainQueue(ctx, &h.steerQueue, h.steeringMode)
}

func (h *AgentHarness) drainFollowUp(ctx context.Context) ([]agent.AgentMessage, error) {
	return h.drainQueue(ctx, &h.followUpQueue, h.followUpMode)
}

func (h *AgentHarness) drainQueue(ctx context.Context, queue *[]agent.AgentMessage, mode agent.QueueMode) ([]agent.AgentMessage, error) {
	if len(*queue) == 0 {
		return nil, nil
	}
	count := len(*queue)
	if mode != agent.QueueModeAll {
		count = 1
	}
	messages := append([]agent.AgentMessage{}, (*queue)[:count]...)
	*queue = append((*queue)[:0], (*queue)[count:]...)
	if err := h.emit(ctx, AgentHarnessEvent{Type: "queue_update", Steer: h.steerQueue, FollowUp: h.followUpQueue, NextTurn: h.nextTurnQueue}); err != nil {
		*queue = append(messages, (*queue)...)
		return nil, err
	}
	return messages, nil
}

func (h *AgentHarness) startRun(ctx context.Context) (context.Context, func()) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	h.runMu.Lock()
	h.nextRunID++
	id := h.nextRunID
	h.activeRunID = id
	h.cancelRun = cancel
	h.runDone = done
	h.runMu.Unlock()
	finish := func() {
		h.runMu.Lock()
		if h.activeRunID == id {
			h.cancelRun = nil
			h.runDone = nil
			h.activeRunID = 0
		}
		close(done)
		h.runMu.Unlock()
	}
	return runCtx, finish
}

func (h *AgentHarness) activeTools() []agent.AgentTool[any] {
	tools := make([]agent.AgentTool[any], 0, len(h.activeToolNames))
	for _, name := range h.activeToolNames {
		if tool, ok := h.tools[name]; ok {
			tools = append(tools, tool)
		}
	}
	return tools
}

func (h *AgentHarness) validateToolNames(names []string) error {
	return validateToolNames(names, h.tools)
}

func (h *AgentHarness) systemPromptOrDefault(ctx context.Context) (string, error) {
	if h.systemPrompt != "" {
		return h.systemPrompt, nil
	}
	if h.systemPromptFunc != nil {
		prompt, err := h.systemPromptFunc(ctx, AgentHarnessSystemPromptContext{
			Session:       h.session,
			Model:         h.model,
			ThinkingLevel: h.thinkingLevel,
			ActiveTools:   h.activeTools(),
		})
		if err != nil {
			return "", err
		}
		return prompt, nil
	}
	return "You are a helpful assistant.", nil
}

func (h *AgentHarness) summaryGenerationOptions(ctx context.Context, customInstructions string) SummaryGenerationOptions {
	apiKey := ""
	if h.getAPIKey != nil {
		key, _ := h.getAPIKey(ctx, h.model.Provider)
		apiKey = key
	}
	return SummaryGenerationOptions{
		Model:              h.model,
		APIKey:             apiKey,
		Headers:            h.streamOptions.Headers,
		StreamFn:           h.streamFn,
		CustomInstructions: customInstructions,
		ThinkingLevel:      h.thinkingLevel,
	}
}

func (h *AgentHarness) emit(ctx context.Context, event AgentHarnessEvent) error {
	for _, subscriber := range h.subscribers {
		if err := subscriber(ctx, event); err != nil {
			return err
		}
	}
	for _, handler := range h.handlers[event.Type] {
		if _, err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (h *AgentHarness) emitHook(ctx context.Context, event AgentHarnessEvent) (any, error) {
	var last any
	for _, handler := range h.handlers[event.Type] {
		result, err := handler(ctx, event)
		if err != nil {
			return nil, err
		}
		if result != nil {
			last = result
		}
	}
	return last, nil
}

func createUserMessage(text string, attachments []ai.ContentBlock) agent.AgentMessage {
	content := []ai.ContentBlock{{Type: "text", Text: text}}
	content = append(content, attachments...)
	return agent.AgentMessage{Role: "user", Content: content, Timestamp: time.Now().UnixMilli()}
}

func branchSummaryTokenBudget(model ai.Model) int {
	contextWindow := model.ContextWindow
	if contextWindow == 0 {
		contextWindow = 128000
	}
	return contextWindow - DefaultCompactionSettings.ReserveTokens
}

func validateToolNames(names []string, tools map[string]agent.AgentTool[any]) error {
	var missing []string
	for _, name := range names {
		if _, ok := tools[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return errors.New("Unknown tool(s): " + stringsJoin(missing, ", "))
	}
	return nil
}

func cloneHarnessStreamOptions(options AgentHarnessStreamOptions) AgentHarnessStreamOptions {
	clone := options
	if options.Headers != nil {
		clone.Headers = map[string]string{}
		for key, value := range options.Headers {
			clone.Headers[key] = value
		}
	}
	if options.Metadata != nil {
		clone.Metadata = map[string]any{}
		for key, value := range options.Metadata {
			clone.Metadata[key] = value
		}
	}
	return clone
}

func mergeStreamOptions(base AgentHarnessStreamOptions, patch AgentHarnessStreamOptions) AgentHarnessStreamOptions {
	result := cloneHarnessStreamOptions(base)
	if patch.Transport != "" {
		result.Transport = patch.Transport
	}
	if patch.TimeoutMs != nil {
		result.TimeoutMs = patch.TimeoutMs
	}
	if patch.MaxRetries != nil {
		result.MaxRetries = patch.MaxRetries
	}
	if patch.MaxRetryDelayMs != nil {
		result.MaxRetryDelayMs = patch.MaxRetryDelayMs
	}
	if patch.CacheRetention != "" {
		result.CacheRetention = patch.CacheRetention
	}
	if patch.Headers != nil {
		result.Headers = cloneStringMap(result.Headers)
		if result.Headers == nil {
			result.Headers = map[string]string{}
		}
		for key, value := range patch.Headers {
			result.Headers[key] = value
		}
	}
	if patch.Metadata != nil {
		result.Metadata = cloneAnyMap(result.Metadata)
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		for key, value := range patch.Metadata {
			result.Metadata[key] = value
		}
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := map[string]string{}
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func mergeStringMaps(base map[string]string, patch map[string]string) map[string]string {
	if base == nil && patch == nil {
		return nil
	}
	merged := cloneStringMap(base)
	if merged == nil {
		merged = map[string]string{}
	}
	for key, value := range patch {
		merged[key] = value
	}
	return merged
}

func cloneAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	clone := map[string]any{}
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func hookErrorStream(model ai.Model, err error) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		message := ai.Message{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonError, ErrorMessage: err.Error(), Usage: ai.EmptyUsage(), Timestamp: time.Now().UnixMilli()}
		stream.Push(ai.AssistantMessageEvent{Type: "error", Reason: ai.StopReasonError, Error: &message})
	}()
	return stream
}

func stringsJoin(values []string, separator string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += separator + value
	}
	return result
}
