package connector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
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
var _ bridgev2.GroupCreatingNetworkAPI = (*Client)(nil)

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

func (cl *Client) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	if createChat {
		return nil, fmt.Errorf("AI sessions are created by bridging an existing Matrix room")
	}
	name := "AI Assistant"
	isBot := true
	return &bridgev2.ResolveIdentifierResponse{
		UserID: aiid.AssistantUserID(aiid.DefaultProvider, "assistant"),
		UserInfo: &bridgev2.UserInfo{
			Name:  &name,
			IsBot: &isBot,
		},
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
	if requestedLoginID := loginIDFromConfig(roomConfig); requestedLoginID != "" && requestedLoginID != cl.UserLogin.ID {
		targetLogin, err := cl.resolveRequestedLogin(ctx, requestedLoginID)
		if err != nil {
			return nil, err
		}
		targetClient, err := cl.clientForLogin(ctx, targetLogin)
		if err != nil {
			return nil, err
		}
		targetPortal, err := cl.switchPortalLogin(ctx, msg.Portal, requestedLoginID)
		if err != nil {
			return nil, err
		}
		msg.Portal = targetPortal
		return targetClient.handleMatrixMessageWithConfig(ctx, msg, roomConfig, roomStateEventID)
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
	assistantEvent, assistantMetadata := cl.assistantEvent(msg.Portal.PortalKey, assistantMessageID, provider.ID, model.ID, runID, descriptor)
	cl.UserLogin.QueueRemoteEvent(assistantEvent)
	if err := streamPublisher.Register(ctx, msg.Portal.MXID, assistantEventID, descriptor); err != nil {
		return nil, err
	}
	defer streamPublisher.Unregister(msg.Portal.MXID, assistantEventID)

	streamFn := cl.streamPublisher(streamPublisher, msg.Portal.MXID, assistantEventID)
	options := harness.AgentHarnessOptions{
		Session:             agentSession,
		Model:               model,
		ThinkingLevel:       agent.ThinkingLevel(roomConfig.ThinkingLevel),
		SystemPrompt:        roomConfig.SystemPrompt,
		StreamFn:            streamFn,
		GetAPIKeyAndHeaders: cl.authForProvider(provider),
	}
	env, err := cl.executionEnv(roomConfig)
	if err != nil {
		return nil, err
	}
	if env != nil {
		options.Env = env
		options.Tools = workspaceTools(env)
	}
	agentHarness, err := harness.NewAgentHarness(options)
	if err != nil {
		return nil, err
	}
	cl.setActiveHarness(msg.Portal.PortalKey, agentHarness)
	defer cl.clearActiveHarness(msg.Portal.PortalKey, agentHarness)

	promptResult, err := agentHarness.PromptWithResult(ctx, prompt.Text, prompt.Images...)
	if err != nil {
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
	_ = streamPublisher.Publish(ctx, msg.Portal.MXID, assistantEventID, map[string]any{
		"op":               "done",
		"text":             msgconv.AssistantText(assistantMessage),
		"stop_reason":      assistantMessage.StopReason,
		"usage":            assistantMessage.Usage,
		"response_id":      assistantMessage.ResponseID,
		"session_entry_id": promptResult.AssistantEntryID,
	})
	fillAssistantMetadata(assistantMetadata, promptResult.AssistantEntryID, provider.ID, model.ID, runID, assistantMessage)
	go cl.updateAssistantMessageMetadata(context.WithoutCancel(ctx), msg.Portal.PortalKey, assistantMessageID, assistantMetadata)
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

func (cl *Client) assistantEvent(portalKey networkid.PortalKey, messageID networkid.MessageID, providerID string, modelID string, runID string, descriptor *event.BeeperStreamInfo) (*simplevent.PreConvertedMessage, *aiid.MessageMetadata) {
	content := msgconv.TextContent(" ")
	content.BeeperStream = descriptor
	metadata := &aiid.MessageMetadata{
		Role:         "assistant",
		ProviderID:   providerID,
		ModelID:      modelID,
		RunID:        runID,
		StreamStatus: "streaming",
	}
	return &simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventMessage,
			PortalKey: portalKey,
			Sender: bridgev2.EventSender{
				Sender: aiid.AssistantUserID(providerID, modelID),
			},
			Timestamp: time.Now(),
		},
		ID: messageID,
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:         aiid.PartID("text"),
			Type:       event.EventMessage,
			Content:    content,
			DBMetadata: metadata,
		}}},
	}, metadata
}

func (cl *Client) streamPublisher(publisher bridgev2.BeeperStreamPublisher, roomID id.RoomID, eventID id.EventID) agent.StreamFn {
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		upstream := ai.StreamSimple(ctx, model, llmContext, options)
		downstream := ai.NewAssistantMessageEventStream()
		go func() {
			defer downstream.End()
			for evt := range upstream.Events() {
				if evt.Type == "done" {
					downstream.Push(evt)
					continue
				}
				if err := publisher.Publish(ctx, roomID, eventID, msgconv.StreamDelta(evt)); err != nil {
					downstream.Push(ai.AssistantMessageEvent{
						Type: "error",
						Error: &ai.Message{
							Role:         "assistant",
							ErrorMessage: err.Error(),
							StopReason:   ai.StopReasonError,
						},
					})
					return
				}
				downstream.Push(evt)
			}
		}()
		return downstream
	}
}

func (cl *Client) sessionForPortal(ctx context.Context, portal *bridgev2.Portal, meta *aiid.PortalMetadata, roomConfig RoomConfig, stateEventID string, providerID string, modelID string) (*session.Session, error) {
	if meta == nil {
		meta = &aiid.PortalMetadata{}
		portal.Metadata = meta
	}
	if meta.SelectedLoginID != "" && meta.SelectedLoginID != string(cl.UserLogin.ID) {
		meta.SessionID = ""
	}
	meta.SelectedLoginID = string(cl.UserLogin.ID)
	meta.SelectedProviderID = providerID
	meta.SelectedModelID = modelID
	meta.SystemPrompt = roomConfig.SystemPrompt
	meta.ThinkingLevel = roomConfig.ThinkingLevel
	meta.ToolsEnabled = roomConfig.ToolsEnabled
	meta.Cwd = roomConfig.Cwd
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
	agentSession, err := cl.Main.Store.CreateSession(ctx, session.SQLiteSessionCreateOptions{Cwd: roomConfig.Cwd})
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

func (cl *Client) executionEnv(config RoomConfig) (*harness.LocalExecutionEnv, error) {
	if !cl.Main.Config.Tools.Enabled || !config.ToolsEnabled || config.Cwd == "" {
		if cl.Main.Config.Tools.Enabled && config.ToolsEnabled {
			return nil, fmt.Errorf("workspace tools require cwd in %s state", cl.Main.Config.RoomStateEventType)
		}
		return nil, nil
	}
	cwd, err := filepath.Abs(config.Cwd)
	if err != nil {
		return nil, err
	}
	for _, root := range cl.Main.Config.Tools.WorkspaceRoots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if cwd == rootAbs || slices.Contains(splitPathParents(cwd), rootAbs) {
			return harness.NewLocalExecutionEnv(cwd), nil
		}
	}
	return nil, fmt.Errorf("cwd %s is outside configured workspace roots", config.Cwd)
}

func splitPathParents(path string) []string {
	parents := []string{}
	current := filepath.Clean(path)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return parents
		}
		parents = append(parents, parent)
		current = parent
	}
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
