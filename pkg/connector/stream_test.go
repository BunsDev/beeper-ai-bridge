package connector

import (
	"context"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestStreamPublisherUsesFakeProviderAndPublishesDeltas(t *testing.T) {
	ctx := context.Background()
	testAPI := ai.Api("test-stream")
	ai.RegisterAPIProvider(testAPI, func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "hello"}}, StopReason: ai.StopReasonStop}
			stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "hel"})
			stream.Push(ai.AssistantMessageEvent{Type: "text_delta", ContentIndex: 0, Delta: "lo"})
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
		}()
		return stream
	})
	defer ai.UnregisterAPIProvider(testAPI)

	publisher := &recordingStreamPublisher{}
	client := &Client{}
	run := aistream.NewRun("run", "thread", "beeper/fake", "assistant:run", "Fake", timeNow())
	run.MessageID = "assistant:run"
	streamFn := client.streamPublisher(publisher, "!room:example.com", "$event", run)
	result := streamFn(ctx, ai.Model{ID: "fake", API: testAPI}, ai.Context{}, ai.SimpleStreamOptions{}).Result()
	if result.StopReason != ai.StopReasonStop {
		t.Fatalf("unexpected stream result %#v", result)
	}
	if len(publisher.updates) != 4 {
		t.Fatalf("expected stream updates, got %#v", publisher.updates)
	}
	deltas, ok := publisher.updates[1][aistream.BeeperAIStreamDeltas].([]aistream.Envelope)
	if !ok || len(deltas) != 1 {
		t.Fatalf("unexpected first delta %#v", publisher.updates[1])
	}
	part := deltas[0].Part
	if part["type"] != agui.EventTextMessageContent || part["delta"] != "hel" {
		t.Fatalf("unexpected first text part %#v", part)
	}
}

func TestAssistantEventMetadataCanBeFinalizedBeforeInsert(t *testing.T) {
	client := &Client{}
	run := aistream.NewRun("run", "thread", "beeper/gpt-5", "assistant:run", "GPT-5", timeNow())
	run.MessageID = "assistant:run"
	assistantEvent, metadata := client.assistantEvent(
		aiid.PortalKey(id.RoomID("!room:example.com"), "login"),
		"assistant:run",
		"beeper",
		"gpt-5",
		"run",
		&event.BeeperStreamInfo{Type: "com.beeper.ai.response"},
		*run,
	)
	if metadata.StreamStatus != "streaming" {
		t.Fatalf("expected streaming metadata, got %#v", metadata)
	}
	fillAssistantMetadata(metadata, "entry", "beeper", "gpt-5", "run", ai.Message{
		Role:       "assistant",
		ResponseID: "resp",
		Usage:      ai.Usage{Input: 1, Output: 2},
		StopReason: ai.StopReasonStop,
	})
	partMetadata, ok := assistantEvent.Data.Parts[0].DBMetadata.(*aiid.MessageMetadata)
	if !ok {
		t.Fatalf("unexpected metadata type %T", assistantEvent.Data.Parts[0].DBMetadata)
	}
	if partMetadata.SessionEntryID != "entry" || partMetadata.StreamStatus != "done" || partMetadata.ResponseID != "resp" {
		t.Fatalf("metadata was not finalized through shared pointer: %#v", partMetadata)
	}
}

type recordingStreamPublisher struct {
	updates []map[string]any
}

func (p *recordingStreamPublisher) NewDescriptor(ctx context.Context, roomID id.RoomID, streamType string) (*event.BeeperStreamInfo, error) {
	return &event.BeeperStreamInfo{UserID: "@bot:example.com", Type: streamType}, nil
}

func (p *recordingStreamPublisher) Register(ctx context.Context, roomID id.RoomID, eventID id.EventID, descriptor *event.BeeperStreamInfo) error {
	return nil
}

func (p *recordingStreamPublisher) Publish(ctx context.Context, roomID id.RoomID, eventID id.EventID, delta map[string]any) error {
	p.updates = append(p.updates, delta)
	return nil
}

func (p *recordingStreamPublisher) Unregister(roomID id.RoomID, eventID id.EventID) {
}

func timeNow() time.Time {
	return time.Unix(1, 0)
}
