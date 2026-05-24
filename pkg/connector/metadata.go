package connector

import (
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2/database"
)

func (c *Connector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Portal: func() any {
			return &aiid.PortalMetadata{}
		},
		Ghost: func() any {
			return &aiid.GhostMetadata{}
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
