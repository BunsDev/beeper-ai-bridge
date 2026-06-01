package connector

import (
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"

	"github.com/beeper/ai-bridge/pkg/aiid"
)

func TestLastKnownTimezoneFromMatrixMessage(t *testing.T) {
	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Event: &event.Event{Content: event.Content{Raw: map[string]any{
				beeperTimezoneKey: " Europe/Amsterdam ",
			}}},
		},
	}
	timezone, ok := lastKnownTimezoneFromMatrixMessage(msg)
	if !ok || timezone != "Europe/Amsterdam" {
		t.Fatalf("timezone = %q ok=%v, want Europe/Amsterdam true", timezone, ok)
	}

	msg.Event.Content.Raw[beeperTimezoneKey] = "Local"
	if timezone, ok = lastKnownTimezoneFromMatrixMessage(msg); ok || timezone != "" {
		t.Fatalf("Local timezone should be rejected, got %q ok=%v", timezone, ok)
	}
}

func TestSetLastKnownTimezoneOnLogin(t *testing.T) {
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{}}
	if !setLastKnownTimezoneOnLogin(login, "Europe/Amsterdam") {
		t.Fatal("expected timezone update")
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta.LastKnownTimezone != "Europe/Amsterdam" {
		t.Fatalf("timezone was not stored in login metadata: %#v", login.Metadata)
	}
	if setLastKnownTimezoneOnLogin(login, "Europe/Amsterdam") {
		t.Fatal("unchanged timezone should not require a save")
	}
}
