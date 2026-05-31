package connector

import (
	"maunium.net/go/mautrix/bridgev2/database"

	"github.com/beeper/ai-bridge/pkg/aiid"
)

func (c *Connector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Portal: func() any {
			return &aiid.PortalMetadata{}
		},
		Message: func() any {
			return &aiid.MessageMetadata{}
		},
		Reaction: func() any {
			return &aiid.ReactionMetadata{}
		},
		UserLogin: func() any {
			return &aiid.UserLoginMetadata{}
		},
	}
}
