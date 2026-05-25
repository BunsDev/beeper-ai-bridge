package msgconv

import (
	"context"
	"strings"
	"testing"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

func TestFromMatrixUsesMatrixHTMLForFormattedText(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), nil, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:       event.MsgText,
				Body:          "plain text",
				FormattedBody: "<strong>plain text</strong>",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != "<strong>plain text</strong>" {
		t.Fatalf("expected Matrix HTML prompt text, got %q", prompt.Text)
	}
}

func TestFromMatrixIncludesReplySessionContext(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), nil, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "follow up"},
		},
		ReplyTo: &database.Message{Metadata: &aiid.MessageMetadata{
			SessionEntryID: "entry-1",
			Role:           "assistant",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.Text, "entry-1") || !strings.Contains(prompt.Text, "follow up") {
		t.Fatalf("expected reply context in prompt, got %q", prompt.Text)
	}
}
