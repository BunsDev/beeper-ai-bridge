package aibridgev2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

func WaitForMessageEventID(ctx context.Context, bridge *bridgev2.Bridge, receiver networkid.UserLoginID, messageID networkid.MessageID, partID networkid.PartID, timeout time.Duration) (id.EventID, error) {
	if bridge == nil || bridge.DB == nil || bridge.DB.Message == nil {
		return "", fmt.Errorf("missing bridge message store")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		message, err := bridge.DB.Message.GetPartByID(ctx, receiver, messageID, partID)
		if err == nil && message != nil && message.MXID != "" {
			return message.MXID, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for message event ID: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
