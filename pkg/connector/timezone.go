package connector

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"

	"github.com/beeper/ai-bridge/pkg/aiid"
)

const beeperTimezoneKey = "com.beeper.tz"

func (cl *Client) updateLastKnownTimezoneFromMessage(ctx context.Context, msg *bridgev2.MatrixMessage) {
	timezone, ok := lastKnownTimezoneFromMatrixMessage(msg)
	if !ok || cl == nil || cl.UserLogin == nil {
		return
	}
	if setLastKnownTimezoneOnLogin(cl.UserLogin, timezone) {
		if err := cl.UserLogin.Save(ctx); err != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Str("timezone", timezone).Msg("Failed to save last known timezone")
		}
	}
}

func (cl *Client) lastKnownTimezone() string {
	if cl == nil || cl.UserLogin == nil {
		return ""
	}
	meta, ok := cl.UserLogin.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		return ""
	}
	return meta.LastKnownTimezone
}

func setLastKnownTimezoneOnLogin(login *bridgev2.UserLogin, timezone string) bool {
	if login == nil || timezone == "" {
		return false
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		meta = &aiid.UserLoginMetadata{}
		login.Metadata = meta
	}
	if meta.LastKnownTimezone == timezone {
		return false
	}
	meta.LastKnownTimezone = timezone
	return true
}

func lastKnownTimezoneFromMatrixMessage(msg *bridgev2.MatrixMessage) (string, bool) {
	if msg == nil || msg.Event == nil || msg.Event.Content.Raw == nil {
		return "", false
	}
	raw, ok := msg.Event.Content.Raw[beeperTimezoneKey].(string)
	if !ok {
		return "", false
	}
	timezone, ok := normalizeLastKnownTimezone(raw)
	return timezone, ok
}

func normalizeLastKnownTimezone(raw string) (string, bool) {
	timezone := strings.TrimSpace(raw)
	if timezone == "" || strings.EqualFold(timezone, "local") {
		return "", false
	}
	if strings.EqualFold(timezone, "utc") {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", false
	}
	return loc.String(), true
}
