package aiid

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

const (
	NetworkID        = "ai"
	BeeperBridgeType = "ai"
	DefaultLoginName = "beeper"
	DefaultProvider  = "beeper"
	RoomConfigType   = "com.beeper.ai.room_config"
	StreamType       = "com.beeper.ai.response"
)

func DefaultLoginID(mxid id.UserID) networkid.UserLoginID {
	return networkid.UserLoginID("default:" + encode(string(mxid)))
}

func CustomLoginID(mxid id.UserID, slug string) networkid.UserLoginID {
	return networkid.UserLoginID("custom:" + encode(string(mxid)) + ":" + sanitizeID(slug))
}

func PortalID(roomID id.RoomID) networkid.PortalID {
	return networkid.PortalID("mxroom:" + encode(string(roomID)))
}

func PortalKey(roomID id.RoomID, loginID networkid.UserLoginID) networkid.PortalKey {
	return networkid.PortalKey{ID: PortalID(roomID), Receiver: loginID}
}

func AssistantUserID(providerID string, modelID string) networkid.UserID {
	return networkid.UserID("assistant:" + sanitizeID(providerID) + ":" + sanitizeID(modelID))
}

func ParseAssistantUserID(userID networkid.UserID) (providerID string, modelID string, ok bool) {
	rest, ok := strings.CutPrefix(string(userID), "assistant:")
	if !ok {
		return "", "", false
	}
	providerID, modelID, ok = strings.Cut(rest, ":")
	if !ok || providerID == "" || modelID == "" {
		return "", "", false
	}
	return providerID, modelID, true
}

func UserMessageID(entryID string) networkid.MessageID {
	return networkid.MessageID("user:" + entryID)
}

func AssistantMessageID(entryID string) networkid.MessageID {
	return networkid.MessageID("assistant:" + entryID)
}

func PartID(name string) networkid.PartID {
	return networkid.PartID(sanitizeID(name))
}

func MediaID(parts ...string) networkid.MediaID {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned = append(cleaned, sanitizeID(part))
	}
	return networkid.MediaID(strings.Join(cleaned, ":"))
}

func MediaIDFor(metadata MediaMetadata) (networkid.MediaID, error) {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return networkid.MediaID(""), err
	}
	return networkid.MediaID("ai:" + base64.RawURLEncoding.EncodeToString(raw)), nil
}

func ParseMediaID(mediaID networkid.MediaID) (MediaMetadata, error) {
	encoded, ok := strings.CutPrefix(string(mediaID), "ai:")
	if !ok {
		return MediaMetadata{}, json.Unmarshal([]byte(mediaID), &MediaMetadata{})
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return MediaMetadata{}, err
	}
	var metadata MediaMetadata
	err = json.Unmarshal(raw, &metadata)
	return metadata, err
}

func encode(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "\n", "_", "\r", "_", "\t", "_")
	return replacer.Replace(value)
}
