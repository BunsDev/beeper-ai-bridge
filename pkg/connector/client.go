package connector

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	agent "github.com/beeper/ai-bridge/pkg/agent"
	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	"github.com/beeper/ai-bridge/pkg/agent/sessiontitle"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	aibridgev2 "github.com/beeper/ai-bridge/pkg/ai-stream/bridgev2"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/msgconv"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type Client struct {
	Main      *Connector
	UserLogin *bridgev2.UserLogin
	loggedIn  bool

	activeMu        sync.Mutex
	activeHarnesses map[networkid.PortalKey]*harness.AgentHarness
}

var _ bridgev2.NetworkAPI = (*Client)(nil)
var _ bridgev2.NetworkAPIWithUserID = (*Client)(nil)
var _ bridgev2.RedactionHandlingNetworkAPI = (*Client)(nil)
var _ bridgev2.RoomTopicHandlingNetworkAPI = (*Client)(nil)
var _ bridgev2.DisappearTimerChangingNetworkAPI = (*Client)(nil)
var _ bridgev2.GroupCreatingNetworkAPI = (*Client)(nil)
var _ bridgev2.IdentifierResolvingNetworkAPI = (*Client)(nil)
var _ bridgev2.ContactListingNetworkAPI = (*Client)(nil)
var _ bridgev2.UserSearchingNetworkAPI = (*Client)(nil)

func (cl *Client) Connect(ctx context.Context) {
	cl.loggedIn = true
}

func (cl *Client) Disconnect() {
	cl.loggedIn = false
}

func (cl *Client) IsLoggedIn() bool {
	return cl.loggedIn
}

func (cl *Client) LogoutRemote(ctx context.Context) {
	cl.Disconnect()
}

func (cl *Client) GetUserID() networkid.UserID {
	return networkid.UserID("login:" + string(cl.UserLogin.ID))
}

func (cl *Client) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return userID == cl.GetUserID()
}

func (cl *Client) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	name := "AI"
	if meta := portalMetadata(portal); meta.SessionTitle != "" {
		name = meta.SessionTitle
	}
	return &bridgev2.ChatInfo{Name: &name}, nil
}

func (cl *Client) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	isBot := true
	name := "AI Assistant"
	if meta, ok := ghost.Metadata.(*aiid.GhostMetadata); ok && meta.ModelID != "" {
		name = meta.ModelID
	} else if _, modelID, ok := aiid.ParseAssistantUserID(ghost.ID); ok {
		name = modelID
	}
	return &bridgev2.UserInfo{
		Name:  &name,
		IsBot: &isBot,
	}, nil
}

func (cl *Client) CreateGroup(ctx context.Context, params *bridgev2.GroupCreateParams) (*bridgev2.CreateChatResponse, error) {
	if params == nil || params.RoomID == "" {
		return nil, fmt.Errorf("AI sessions must be created from an existing Matrix room")
	}
	if params.Type != "" && params.Type != "ai" {
		return nil, fmt.Errorf("unsupported AI group type %s", params.Type)
	}
	name := "AI"
	if params.Name != nil && params.Name.Name != "" {
		name = params.Name.Name
	}
	return &bridgev2.CreateChatResponse{
		PortalKey: aiid.PortalKey(params.RoomID, cl.UserLogin.ID),
		PortalInfo: &bridgev2.ChatInfo{
			Name: &name,
		},
	}, nil
}

func (cl *Client) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	resp, err := cl.handleMatrixMessage(ctx, msg)
	if err != nil && msg != nil && msg.Portal != nil {
		cl.logMatrixMessageError(msg, err, "AI prompt failed")
		cl.queueMatrixError(msg.Portal, err)
	}
	return resp, err
}

func (cl *Client) handleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	if err := cl.ensureUsablePortal(msg.Portal); err != nil {
		return nil, err
	}
	portalMeta := portalMetadata(msg.Portal)
	roomConfig, roomStateEventID, err := cl.Main.ReadRoomConfig(ctx, msg.Portal.MXID, portalMeta)
	if err != nil {
		return nil, err
	}
	return cl.handleMatrixMessageWithConfig(ctx, msg, roomConfig, roomStateEventID)
}

func (cl *Client) handleMatrixMessageWithConfig(ctx context.Context, msg *bridgev2.MatrixMessage, roomConfig RoomConfig, roomStateEventID string) (*bridgev2.MatrixMessageResponse, error) {
	portalMeta := portalMetadata(msg.Portal)
	provider, modelID, err := cl.Main.ResolveProvider(ctx, cl.UserLogin, roomConfig)
	if err != nil {
		return nil, err
	}
	model := cl.Main.ModelForProvider(provider, modelID)
	if err := cl.validateReasoningLevel(model, roomConfig); err != nil {
		return nil, err
	}
	prompt, err := msgconv.FromMatrix(ctx, cl.Main.Bridge.Matrix.BotIntent(), msg)
	if err != nil {
		return nil, err
	}
	if len(prompt.Images) > 0 && !isImageModel(model) {
		return nil, fmt.Errorf("model %s does not support image input", model.ID)
	}
	agentSession, err := cl.sessionForPortal(ctx, msg.Portal, portalMeta, roomConfig, roomStateEventID, provider.ID, model.ID)
	if err != nil {
		return nil, err
	}
	streamPublisher := cl.Main.Bridge.GetBeeperStreamPublisher()
	runID := session.CreateSessionID()
	assistantMessageID := aiid.AssistantMessageID(runID)
	run := aistream.NewRun(runID, portalMeta.SessionID, provider.ID+"/"+model.ID, string(aiid.AssistantUserID(provider.ID, model.ID)), provider.DisplayName, time.Now())
	run.MessageID = string(assistantMessageID)
	assistantEventID := cl.Main.Bridge.Matrix.GenerateDeterministicEventID(
		msg.Portal.MXID,
		msg.Portal.PortalKey,
		assistantMessageID,
		aiid.PartID("text"),
	)
	descriptor, err := streamPublisher.NewDescriptor(ctx, msg.Portal.MXID, cl.Main.Config.StreamType)
	if err != nil {
		return nil, err
	}
	assistantEvent, assistantMetadata := cl.assistantEvent(msg.Portal.PortalKey, assistantMessageID, provider.ID, model.ID, runID, descriptor, *run)
	cl.UserLogin.QueueRemoteEvent(assistantEvent)
	if err := streamPublisher.Register(ctx, msg.Portal.MXID, assistantEventID, descriptor); err != nil {
		return nil, err
	}
	defer streamPublisher.Unregister(msg.Portal.MXID, assistantEventID)

	streamFn := cl.streamPublisher(streamPublisher, msg.Portal.MXID, assistantEventID, run)
	options := harness.AgentHarnessOptions{
		Session:             agentSession,
		Model:               model,
		ThinkingLevel:       agent.ThinkingLevel(cl.reasoningLevel(roomConfig)),
		SystemPrompt:        cl.systemPrompt(roomConfig),
		Tools:               cl.chatTools(msg, portalMeta, roomConfig, provider, model, prompt),
		StreamFn:            streamFn,
		GetAPIKeyAndHeaders: cl.authForProvider(provider),
	}
	agentHarness, err := harness.NewAgentHarness(options)
	if err != nil {
		return nil, err
	}
	cl.setActiveHarness(msg.Portal.PortalKey, agentHarness)
	defer cl.clearActiveHarness(msg.Portal.PortalKey, agentHarness)
	cl.queueAssistantTyping(msg.Portal.PortalKey, provider.ID, model.ID, 30*time.Second)
	defer cl.queueAssistantTyping(msg.Portal.PortalKey, provider.ID, model.ID, 0)
	var toolOutputs []toolOutputEvent
	unsubscribeToolOutputs := agentHarness.Subscribe(func(ctx context.Context, event harness.AgentHarnessEvent) error {
		if event.Type != "tool_execution_end" || event.AgentEvent == nil || event.AgentEvent.ToolCallID == "" {
			return nil
		}
		result, ok := event.AgentEvent.Result.(agent.AgentToolResult[any])
		if !ok {
			return nil
		}
		toolOutputs = append(toolOutputs, toolOutputEvent{
			ID:      event.AgentEvent.ToolCallID,
			Name:    event.AgentEvent.ToolName,
			Input:   event.AgentEvent.Args,
			Result:  result,
			IsError: event.AgentEvent.IsError,
		})
		return nil
	})
	defer unsubscribeToolOutputs()

	promptResult, err := agentHarness.PromptWithResult(ctx, prompt.Text, prompt.Images...)
	if err != nil {
		cl.logMatrixMessageError(msg, err, "AI harness prompt failed")
		_ = streamPublisher.Publish(ctx, msg.Portal.MXID, assistantEventID, map[string]any{
			"op":      "error",
			"message": err.Error(),
		})
		return nil, err
	}
	assistantMessage := promptResult.Message
	if promptResult.UserEntryID == "" || promptResult.AssistantEntryID == "" {
		return nil, fmt.Errorf("prompt did not create expected user and assistant session entries")
	}
	fillAssistantMetadata(assistantMetadata, promptResult.AssistantEntryID, provider.ID, model.ID, runID, assistantMessage)
	appendToolOutputs(run, toolOutputs)
	go cl.updateAssistantMessageMetadata(context.WithoutCancel(ctx), msg.Portal.PortalKey, assistantMessageID, assistantMetadata)
	cl.UserLogin.QueueRemoteEvent(cl.assistantFinalEdit(msg.Portal.PortalKey, assistantMessageID, provider.ID, model.ID, runID, *run, assistantMessage, assistantMetadata))
	cl.generateSessionTitle(ctx, msg.Portal, portalMeta, agentSession, provider, model)
	cl.runAutoCompaction(ctx, streamPublisher, msg.Portal.MXID, assistantEventID, agentHarness, agentSession, model, assistantMessage)
	portalMeta.LastRunID = runID
	_ = msg.Portal.Save(ctx)
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        aiid.UserMessageID(promptResult.UserEntryID),
			PartID:    aiid.PartID("text"),
			Room:      msg.Portal.PortalKey,
			SenderID:  cl.GetUserID(),
			Timestamp: matrixEventTime(msg.Event),
			Metadata: &aiid.MessageMetadata{
				SessionEntryID: promptResult.UserEntryID,
				Role:           "user",
				ProviderID:     provider.ID,
				ModelID:        model.ID,
				RunID:          runID,
				StreamStatus:   "done",
			},
		},
	}, nil
}

func (cl *Client) generateSessionTitle(ctx context.Context, portal *bridgev2.Portal, meta *aiid.PortalMetadata, agentSession *session.Session, provider aiid.ProviderConfig, model ai.Model) {
	if meta.SessionTitle != "" {
		return
	}
	existingName, err := agentSession.GetSessionName(ctx)
	if err != nil || existingName != nil {
		if existingName != nil {
			meta.SessionTitle = *existingName
		}
		return
	}
	contextView, err := agentSession.BuildContext(ctx)
	if err != nil || len(contextView.Messages) < 2 {
		return
	}
	auth, err := cl.authForProvider(provider)(ctx, model)
	if err != nil {
		return
	}
	title, err := sessiontitle.Generate(ctx, contextView.Messages, sessiontitle.Options{
		Model:   model,
		APIKey:  auth.APIKey,
		Headers: auth.Headers,
	})
	if err != nil || title == "" {
		return
	}
	if _, err = agentSession.AppendSessionName(ctx, title); err != nil {
		return
	}
	meta.SessionTitle = title
	portal.UpdateInfo(ctx, &bridgev2.ChatInfo{Name: &title}, cl.UserLogin, nil, time.Now())
}

func (cl *Client) queueAssistantTyping(portalKey networkid.PortalKey, providerID string, modelID string, timeout time.Duration) {
	cl.UserLogin.QueueRemoteEvent(&simplevent.Typing{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventTyping,
			PortalKey: portalKey,
			Sender: bridgev2.EventSender{
				Sender: aiid.AssistantUserID(providerID, modelID),
			},
			Timestamp: time.Now(),
		},
		Timeout: timeout,
		Type:    bridgev2.TypingTypeText,
	})
}

func fillAssistantMetadata(metadata *aiid.MessageMetadata, entryID string, providerID string, modelID string, runID string, assistantMessage ai.Message) {
	metadata.SessionEntryID = entryID
	metadata.Role = "assistant"
	metadata.ProviderID = providerID
	metadata.ModelID = modelID
	metadata.ResponseID = assistantMessage.ResponseID
	metadata.RunID = runID
	metadata.Usage = assistantMessage.Usage
	metadata.StopReason = string(assistantMessage.StopReason)
	metadata.StreamStatus = "done"
}

func (cl *Client) updateAssistantMessageMetadata(ctx context.Context, portalKey networkid.PortalKey, messageID networkid.MessageID, metadata *aiid.MessageMetadata) {
	if cl.Main == nil || cl.Main.Bridge == nil || cl.Main.Bridge.DB == nil || cl.Main.Bridge.DB.Message == nil {
		return
	}
	for attempt := 0; attempt < 20; attempt++ {
		dbMessage, err := cl.Main.Bridge.DB.Message.GetPartByID(ctx, portalKey.Receiver, messageID, aiid.PartID("text"))
		if err != nil {
			return
		}
		if dbMessage != nil {
			dbMessage.Metadata = metadata
			_ = cl.Main.Bridge.DB.Message.Update(ctx, dbMessage)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (cl *Client) queueMatrixError(portal *bridgev2.Portal, err error) {
	messageID := networkid.MessageID("error:" + session.CreateSessionID())
	cl.UserLogin.QueueRemoteEvent(&simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventMessage,
			PortalKey: portal.PortalKey,
			Sender: bridgev2.EventSender{
				Sender: aiid.AssistantUserID("system", "error"),
			},
			Timestamp: time.Now(),
		},
		ID: messageID,
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      aiid.PartID("error"),
			Type:    event.EventMessage,
			Content: msgconv.NoticeContent(err.Error()),
			DBMetadata: &aiid.MessageMetadata{
				Role:         "assistant",
				ErrorMessage: err.Error(),
				StreamStatus: "error",
			},
		}}},
	})
}

func (cl *Client) HandleMatrixMessageRemove(ctx context.Context, msg *bridgev2.MatrixMessageRemove) error {
	if msg == nil || msg.Portal == nil {
		return nil
	}
	h := cl.getActiveHarness(msg.Portal.PortalKey)
	if h == nil {
		return nil
	}
	_, err := h.Abort(ctx)
	return err
}

func (cl *Client) HandleMatrixRoomTopic(ctx context.Context, msg *bridgev2.MatrixRoomTopic) (bool, error) {
	if msg == nil || msg.Portal == nil || msg.Content == nil {
		return false, nil
	}
	topic := msg.Content.Topic
	msg.Portal.Topic = topic
	msg.Portal.TopicSet = topic != ""
	meta := portalMetadata(msg.Portal)
	if meta.SessionID != "" {
		agentSession, err := cl.Main.Store.OpenSession(ctx, session.SQLiteSessionMetadata{
			SessionMetadata: session.SessionMetadata{ID: meta.SessionID},
		})
		if err == nil {
			_, _ = agentSession.AppendCustomMessageEntry(ctx, "room_topic", map[string]any{"topic": topic}, false, nil)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	return true, nil
}

func (cl *Client) HandleMatrixDisappearingTimer(ctx context.Context, msg *bridgev2.MatrixDisappearingTimer) (bool, error) {
	if msg == nil || msg.Portal == nil {
		return false, nil
	}
	msg.Portal.Disappear = database.DisappearingSettingFromEvent(msg.Content)
	return true, nil
}

func (cl *Client) resolveRequestedLogin(ctx context.Context, requested networkid.UserLoginID) (*bridgev2.UserLogin, error) {
	if cl.UserLogin.User != nil {
		return cl.Main.ResolveLogin(ctx, cl.UserLogin.User, requested)
	}
	login, err := cl.Main.Bridge.GetExistingUserLoginByID(ctx, requested)
	if err != nil {
		return nil, err
	}
	if login == nil || login.UserMXID != cl.UserLogin.UserMXID {
		return nil, fmt.Errorf("unknown or inaccessible login %s", requested)
	}
	return login, nil
}

func (cl *Client) clientForLogin(ctx context.Context, login *bridgev2.UserLogin) (*Client, error) {
	if login.Client == nil {
		if err := cl.Main.LoadUserLogin(ctx, login); err != nil {
			return nil, err
		}
	}
	target, ok := login.Client.(*Client)
	if !ok || target == nil {
		return nil, fmt.Errorf("login %s is not an AI bridge login", login.ID)
	}
	return target, nil
}

func (cl *Client) switchPortalLogin(ctx context.Context, portal *bridgev2.Portal, loginID networkid.UserLoginID) (*bridgev2.Portal, error) {
	targetKey := portal.PortalKey
	targetKey.Receiver = loginID
	_, targetPortal, err := cl.Main.Bridge.ReIDPortal(ctx, portal.PortalKey, targetKey)
	if err != nil {
		return nil, err
	}
	if targetPortal != nil {
		return targetPortal, nil
	}
	targetPortal, err = cl.Main.Bridge.GetPortalByKey(ctx, targetKey)
	if err != nil {
		return nil, err
	}
	if targetPortal.MXID == "" {
		targetPortal.MXID = portal.MXID
		if err := targetPortal.Save(ctx); err != nil {
			return nil, err
		}
	}
	return targetPortal, nil
}

func (cl *Client) ensureUsablePortal(portal *bridgev2.Portal) error {
	if portal == nil {
		return fmt.Errorf("missing portal")
	}
	if portal.Receiver != "" && portal.Receiver != cl.UserLogin.ID {
		return fmt.Errorf("portal receiver %s does not match login %s", portal.Receiver, cl.UserLogin.ID)
	}
	return nil
}

func (cl *Client) assistantEvent(portalKey networkid.PortalKey, messageID networkid.MessageID, providerID string, modelID string, runID string, descriptor *event.BeeperStreamInfo, run aistream.Run) (*simplevent.PreConvertedMessage, *aiid.MessageMetadata) {
	metadata := &aiid.MessageMetadata{
		Role:         "assistant",
		ProviderID:   providerID,
		ModelID:      modelID,
		RunID:        runID,
		StreamStatus: "streaming",
	}
	msg := aibridgev2.Anchor(portalKey, aiid.AssistantUserID(providerID, modelID), run, time.Now())
	if len(msg.Data.Parts) > 0 {
		msg.Data.Parts[0].ID = aiid.PartID("text")
		msg.Data.Parts[0].Content.BeeperStream = descriptor
		msg.Data.Parts[0].DBMetadata = metadata
	}
	return msg, metadata
}

func (cl *Client) assistantFinalEdit(portalKey networkid.PortalKey, messageID networkid.MessageID, providerID string, modelID string, runID string, run aistream.Run, message ai.Message, metadata *aiid.MessageMetadata) *simplevent.Message[*aistream.Run] {
	if message.StopReason == ai.StopReasonError {
		run.Status = aistream.Status{State: "error", FinishReason: string(message.StopReason), Error: map[string]any{"message": message.ErrorMessage}}
	} else if run.Status.State == "streaming" {
		run.Status = aistream.Status{State: "complete", FinishReason: string(message.StopReason)}
	}
	run.Usage = aguiUsage(message.Usage)
	if run.Preview.Text == "" {
		run.Preview = aistream.PreviewFromText(msgconv.AssistantText(message), aistream.PreviewBudgetBytes)
	}
	edit := aibridgev2.FinalMetadataEdit(portalKey, aiid.AssistantUserID(providerID, modelID), messageID, run, time.Now())
	originalConvert := edit.ConvertEditFunc
	edit.ConvertEditFunc = func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message, data *aistream.Run) (*bridgev2.ConvertedEdit, error) {
		converted, err := originalConvert(ctx, portal, intent, existing, data)
		if err != nil || converted == nil {
			return converted, err
		}
		if len(existing) > 0 {
			existing[0].Metadata = metadata
		}
		return converted, nil
	}
	return edit
}

func (cl *Client) streamPublisher(publisher bridgev2.BeeperStreamPublisher, roomID id.RoomID, eventID id.EventID, run *aistream.Run) agent.StreamFn {
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		upstream := ai.StreamSimple(ctx, model, llmContext, options)
		downstream := ai.NewAssistantMessageEventStream()
		writer := aistream.NewWriter(run, time.Now)
		writer.Start()
		published := 0
		nextSeq := 1
		publishNew := func() error {
			if published >= len(run.Events) {
				return nil
			}
			partial := *run
			partial.Events = append([]agui.Event(nil), run.Events[published:]...)
			carriers, err := aistream.PackRunFromSeq(partial, string(eventID), aistream.CarrierBudgetBytes, nextSeq)
			if err != nil {
				return err
			}
			for _, carrier := range carriers {
				if err := publisher.Publish(ctx, roomID, eventID, aistream.CarrierContent(carrier.Envelopes)); err != nil {
					return err
				}
			}
			nextSeq = aistream.NextSeq(carriers)
			published = len(run.Events)
			return nil
		}
		_ = publishNew()
		go func() {
			defer downstream.End()
			for evt := range upstream.Events() {
				applyAIStreamEvent(writer, evt)
				if err := publishNew(); err != nil {
					downstream.Push(ai.AssistantMessageEvent{
						Type: "error",
						Error: &ai.Message{
							Role:         "assistant",
							ErrorMessage: err.Error(),
							StopReason:   ai.StopReasonError,
						},
					})
					cl.logStreamError(err, roomID, eventID, run, "Failed to publish AI stream carrier")
					return
				}
				downstream.Push(evt)
			}
		}()
		return downstream
	}
}

func (cl *Client) logMatrixMessageError(msg *bridgev2.MatrixMessage, err error, message string) {
	if cl == nil || cl.Main == nil || cl.Main.Bridge == nil {
		return
	}
	event := cl.Main.Bridge.Log.Error().Err(err)
	if msg != nil {
		if msg.Portal != nil {
			event = event.
				Str("portal_id", string(msg.Portal.ID)).
				Str("portal_receiver", string(msg.Portal.Receiver)).
				Str("portal_mxid", string(msg.Portal.MXID))
		}
		if msg.Event != nil {
			event = event.
				Str("event_id", string(msg.Event.ID)).
				Str("event_type", string(msg.Event.Type.Type)).
				Str("sender", string(msg.Event.Sender))
		}
	}
	if cl.UserLogin != nil {
		event = event.Str("login_id", string(cl.UserLogin.ID))
	}
	event.Msg(message)
}

func (cl *Client) logStreamError(err error, roomID id.RoomID, eventID id.EventID, run *aistream.Run, message string) {
	if cl == nil || cl.Main == nil || cl.Main.Bridge == nil {
		return
	}
	event := cl.Main.Bridge.Log.Error().
		Err(err).
		Str("room_id", string(roomID)).
		Str("event_id", string(eventID))
	if run != nil {
		event = event.
			Str("run_id", run.RunID).
			Str("thread_id", run.ThreadID).
			Str("message_id", run.MessageID).
			Str("model", run.Model)
	}
	if cl.UserLogin != nil {
		event = event.Str("login_id", string(cl.UserLogin.ID))
	}
	event.Msg(message)
}

func applyAIStreamEvent(writer *aistream.Writer, evt ai.AssistantMessageEvent) {
	switch evt.Type {
	case "text_delta":
		writer.Text(evt.Delta)
	case "thinking_delta":
		writer.Thinking(evt.Delta)
	case "toolcall_start":
		if evt.ToolCall != nil {
			writer.ToolStart(evt.ToolCall.ID, evt.ToolCall.Name, evt.ContentIndex, nil)
		}
	case "toolcall_delta":
		if evt.ToolCall != nil {
			writer.ToolArgs(evt.ToolCall.ID, evt.Delta, evt.ToolCall.Arguments)
		}
	case "toolcall_end":
		if evt.ToolCall != nil {
			writer.ToolEnd(evt.ToolCall.ID, evt.ToolCall.Name, evt.ToolCall.Arguments, nil)
		}
	case "done":
		if evt.Message != nil {
			usage := aguiUsage(evt.Message.Usage)
			writer.FinishWithUsage(string(evt.Reason), &usage)
		} else {
			writer.Finish(string(evt.Reason))
		}
	case "error":
		message := "stream error"
		if evt.Error != nil && evt.Error.ErrorMessage != "" {
			message = evt.Error.ErrorMessage
		}
		writer.Error(message)
	}
}

type toolOutputEvent struct {
	ID      string
	Name    string
	Input   any
	Result  agent.AgentToolResult[any]
	IsError bool
}

func appendToolOutputs(run *aistream.Run, outputs []toolOutputEvent) {
	if run == nil || len(outputs) == 0 {
		return
	}
	writer := aistream.NewWriter(run, time.Now)
	for _, output := range outputs {
		writer.ToolEnd(output.ID, output.Name, output.Input, toolOutput(output.Result, output.IsError))
	}
}

func toolOutput(result agent.AgentToolResult[any], isError bool) any {
	output := mapFromAny(result.Details)
	if output == nil {
		if text := textFromBlocks(result.Content); text != "" {
			output = map[string]any{"content": text}
		} else {
			output = map[string]any{}
		}
	}
	if isError {
		output["state"] = agui.ToolResultStateError
		output["status"] = "failed"
	} else {
		output["state"] = agui.ToolResultStateComplete
		output["status"] = "success"
	}
	return output
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if source, ok := value.(map[string]any); ok {
		clone := map[string]any{}
		for key, item := range source {
			clone[key] = item
		}
		return clone
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"result": value}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err == nil && out != nil {
		return out
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		return map[string]any{"result": decoded}
	}
	return map[string]any{"result": value}
}

func textFromBlocks(blocks []ai.ContentBlock) string {
	var out string
	for _, block := range blocks {
		if block.Type == "text" {
			out += block.Text
		}
	}
	return out
}

func aguiUsage(usage ai.Usage) agui.Usage {
	total := usage.TotalTokens
	if total == 0 {
		total = usage.Input + usage.Output
	}
	return agui.Usage{
		PromptTokens:     usage.Input,
		CompletionTokens: usage.Output,
		ReasoningTokens:  usage.ReasoningTokens,
		TotalTokens:      total,
	}
}

func (cl *Client) oldAssistantFinalEdit(portalKey networkid.PortalKey, messageID networkid.MessageID, providerID string, modelID string, runID string, message ai.Message, metadata *aiid.MessageMetadata) *simplevent.Message[*bridgev2.ConvertedEdit] {
	content := msgconv.TextContent(msgconv.AssistantText(message))
	return &simplevent.Message[*bridgev2.ConvertedEdit]{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventEdit,
			PortalKey: portalKey,
			Sender: bridgev2.EventSender{
				Sender: aiid.AssistantUserID(providerID, modelID),
			},
			Timestamp: time.Now(),
		},
		ID:            messageID,
		TargetMessage: messageID,
		Data: &bridgev2.ConvertedEdit{ModifiedParts: []*bridgev2.ConvertedEditPart{{
			Type:          event.EventMessage,
			Content:       content,
			TopLevelExtra: map[string]any{"com.beeper.dont_render_edited": true},
		}}},
		ConvertEditFunc: func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message, data *bridgev2.ConvertedEdit) (*bridgev2.ConvertedEdit, error) {
			if len(existing) == 0 {
				return nil, fmt.Errorf("missing existing assistant message %s for final edit", messageID)
			}
			existing[0].Metadata = metadata
			data.ModifiedParts[0].Part = existing[0]
			return data, nil
		},
	}
}

func (cl *Client) sessionForPortal(ctx context.Context, portal *bridgev2.Portal, meta *aiid.PortalMetadata, roomConfig RoomConfig, stateEventID string, providerID string, modelID string) (*session.Session, error) {
	if meta == nil {
		meta = &aiid.PortalMetadata{}
		portal.Metadata = meta
	}
	meta.SelectedProviderID = providerID
	meta.SelectedModelID = modelID
	meta.AdditionalPrompt = roomConfig.AdditionalPrompt
	meta.ThinkingLevel = cl.reasoningLevel(roomConfig)
	meta.DisabledTools = roomConfig.DisabledTools
	meta.RoomStateEventID = stateEventID
	if meta.SessionID != "" {
		agentSession, err := cl.Main.Store.OpenSession(ctx, session.SQLiteSessionMetadata{
			SessionMetadata: session.SessionMetadata{ID: meta.SessionID},
		})
		if err == nil {
			_ = portal.Save(ctx)
			return agentSession, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	agentSession, err := cl.Main.Store.CreateSession(ctx, session.SQLiteSessionCreateOptions{})
	if err != nil {
		return nil, err
	}
	sessionMeta, err := agentSession.GetMetadata(ctx)
	if err != nil {
		return nil, err
	}
	meta.SessionID = sessionMeta.ID
	if err := portal.Save(ctx); err != nil {
		return nil, err
	}
	return agentSession, nil
}

func portalMetadata(portal *bridgev2.Portal) *aiid.PortalMetadata {
	if portal == nil {
		return &aiid.PortalMetadata{}
	}
	if meta, ok := portal.Metadata.(*aiid.PortalMetadata); ok && meta != nil {
		return meta
	}
	meta := &aiid.PortalMetadata{}
	portal.Metadata = meta
	return meta
}

func matrixEventTime(evt *event.Event) time.Time {
	if evt == nil || evt.Timestamp == 0 {
		return time.Now()
	}
	return time.UnixMilli(evt.Timestamp)
}

func (cl *Client) setActiveHarness(key networkid.PortalKey, h *harness.AgentHarness) {
	cl.activeMu.Lock()
	defer cl.activeMu.Unlock()
	if cl.activeHarnesses == nil {
		cl.activeHarnesses = map[networkid.PortalKey]*harness.AgentHarness{}
	}
	cl.activeHarnesses[key] = h
}

func (cl *Client) getActiveHarness(key networkid.PortalKey) *harness.AgentHarness {
	cl.activeMu.Lock()
	defer cl.activeMu.Unlock()
	if cl.activeHarnesses == nil {
		return nil
	}
	return cl.activeHarnesses[key]
}

func (cl *Client) clearActiveHarness(key networkid.PortalKey, h *harness.AgentHarness) {
	cl.activeMu.Lock()
	defer cl.activeMu.Unlock()
	if cl.activeHarnesses != nil && cl.activeHarnesses[key] == h {
		delete(cl.activeHarnesses, key)
	}
}
