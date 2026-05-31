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
	ModelPrefix      = "model:"
	ModelProviderSep = ":"
	RoomToolsType    = "com.beeper.ai.tools"
	RoomModelType    = "com.beeper.ai.model"
	RoomPromptType   = "com.beeper.ai.additional_prompt"
	StreamType       = "com.beeper.stream"
)

func DefaultLoginID(mxid id.UserID) networkid.UserLoginID {
	return networkid.UserLoginID("default:" + encode(string(mxid)))
}

func PortalID(roomID id.RoomID) networkid.PortalID {
	return networkid.PortalID("mxroom:" + encode(string(roomID)))
}

func PortalKey(roomID id.RoomID, loginID networkid.UserLoginID) networkid.PortalKey {
	return networkid.PortalKey{ID: PortalID(roomID), Receiver: loginID}
}

func AssistantUserID() networkid.UserID {
	return networkid.UserID("assistant:ai")
}

func ModelContactID(providerID string, modelID string) networkid.UserID {
	if providerID == "" || providerID == DefaultProvider {
		return networkid.UserID(ModelPrefix + id.EncodeUserLocalpart(modelID))
	}
	return networkid.UserID(ModelPrefix + id.EncodeUserLocalpart(providerID) + ModelProviderSep + id.EncodeUserLocalpart(modelID))
}

func ParseModelContactID(userID networkid.UserID) (providerID string, modelID string, ok bool) {
	rest, ok := strings.CutPrefix(string(userID), ModelPrefix)
	if !ok {
		return "", "", false
	}
	if rest == "" {
		return "", "", false
	}
	providerPart, modelPart, hasProvider := strings.Cut(rest, ModelProviderSep)
	if !hasProvider {
		modelID, ok = decodeModelContactPart(rest)
		if !ok {
			return "", "", false
		}
		return DefaultProvider, modelID, true
	}
	if providerPart == "" || modelPart == "" {
		return "", "", false
	}
	providerID, ok = decodeModelContactPart(providerPart)
	if !ok {
		return "", "", false
	}
	modelID, ok = decodeModelContactPart(modelPart)
	if !ok {
		return "", "", false
	}
	return providerID, modelID, true
}

func decodeModelContactPart(part string) (string, bool) {
	decoded, err := id.DecodeUserLocalpart(part)
	if err != nil {
		return "", false
	}
	return decoded, decoded != ""
}

func ProviderModelIdentifier(providerID string, modelID string) string {
	if providerID == "" || providerID == DefaultProvider {
		return modelID
	}
	return providerID + "/" + modelID
}

func ModelContactIdentifiers(providerID string, modelID string) []string {
	identifier := ProviderModelIdentifier(providerID, modelID)
	if identifier == modelID {
		return []string{modelID}
	}
	return []string{identifier, modelID}
}

func MatchesModelIdentifier(providerID string, modelID string, identifier string) bool {
	return identifier == string(ModelContactID(providerID, modelID)) || identifier == modelID
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
