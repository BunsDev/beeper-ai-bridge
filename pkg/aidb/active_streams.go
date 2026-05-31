package aidb

import (
	"context"
	"encoding/json"
	"time"

	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

type ActiveStreamRecord struct {
	RunID      string
	LoginID    networkid.UserLoginID
	PortalKey  networkid.PortalKey
	RoomID     id.RoomID
	EventID    id.EventID
	MessageID  networkid.MessageID
	ProviderID string
	ModelID    string
	EntryID    string
	Run        aistream.Run
	Metadata   aiid.MessageMetadata
	StatusInfo bridgev2.MessageStatusEventInfo
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (s *Store) UpsertActiveStream(ctx context.Context, record ActiveStreamRecord) error {
	now := record.UpdatedAt
	if now.IsZero() {
		now = time.Now()
	}
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	if record.RunID == "" {
		record.RunID = record.Run.RunID
	}
	runJSON, err := json.Marshal(record.Run)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	statusInfoJSON, err := json.Marshal(record.StatusInfo)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO ai_active_stream (
			bridge_id, run_id, login_id, portal_id, portal_receiver, room_id, event_id, message_id,
			provider_id, model_id, entry_id, run_json, metadata_json, status_info_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (bridge_id, login_id, run_id) DO UPDATE SET
			login_id=excluded.login_id,
			portal_id=excluded.portal_id,
			portal_receiver=excluded.portal_receiver,
			room_id=excluded.room_id,
			event_id=excluded.event_id,
			message_id=excluded.message_id,
			provider_id=excluded.provider_id,
			model_id=excluded.model_id,
			entry_id=excluded.entry_id,
			run_json=excluded.run_json,
			metadata_json=excluded.metadata_json,
			status_info_json=excluded.status_info_json,
			updated_at=excluded.updated_at
	`, s.bridgeID, record.RunID, record.LoginID, record.PortalKey.ID, record.PortalKey.Receiver, record.RoomID, record.EventID, record.MessageID, record.ProviderID, record.ModelID, record.EntryID, string(runJSON), string(metadataJSON), string(statusInfoJSON), createdAt.UnixMilli(), now.UnixMilli())
	return err
}

func (s *Store) DeleteActiveStream(ctx context.Context, loginID networkid.UserLoginID, runID string) error {
	if runID == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `DELETE FROM ai_active_stream WHERE bridge_id=$1 AND login_id=$2 AND run_id=$3`, s.bridgeID, loginID, runID)
	return err
}

func (s *Store) ListStaleActiveStreams(ctx context.Context, loginID networkid.UserLoginID, cutoff time.Time) ([]ActiveStreamRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT run_id, login_id, portal_id, portal_receiver, room_id, event_id, message_id,
		       provider_id, model_id, entry_id, run_json, metadata_json, status_info_json, created_at, updated_at
		FROM ai_active_stream
		WHERE bridge_id=$1 AND login_id=$2 AND updated_at <= $3
		ORDER BY updated_at
	`, s.bridgeID, loginID, cutoff.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ActiveStreamRecord
	for rows.Next() {
		record, err := scanActiveStream(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) ListActiveStreams(ctx context.Context, loginID networkid.UserLoginID) ([]ActiveStreamRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT run_id, login_id, portal_id, portal_receiver, room_id, event_id, message_id,
		       provider_id, model_id, entry_id, run_json, metadata_json, status_info_json, created_at, updated_at
		FROM ai_active_stream
		WHERE bridge_id=$1 AND login_id=$2
		ORDER BY updated_at
	`, s.bridgeID, loginID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ActiveStreamRecord
	for rows.Next() {
		record, err := scanActiveStream(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type activeStreamScanner interface {
	Scan(dest ...any) error
}

func scanActiveStream(row activeStreamScanner) (ActiveStreamRecord, error) {
	var record ActiveStreamRecord
	var portalID, portalReceiver string
	var createdAt, updatedAt int64
	var runJSON, metadataJSON, statusInfoJSON string
	if err := row.Scan(
		&record.RunID,
		&record.LoginID,
		&portalID,
		&portalReceiver,
		&record.RoomID,
		&record.EventID,
		&record.MessageID,
		&record.ProviderID,
		&record.ModelID,
		&record.EntryID,
		&runJSON,
		&metadataJSON,
		&statusInfoJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return record, err
	}
	record.PortalKey = networkid.PortalKey{ID: networkid.PortalID(portalID), Receiver: networkid.UserLoginID(portalReceiver)}
	if err := json.Unmarshal([]byte(runJSON), &record.Run); err != nil {
		return record, err
	}
	if err := json.Unmarshal([]byte(metadataJSON), &record.Metadata); err != nil {
		return record, err
	}
	if err := json.Unmarshal([]byte(statusInfoJSON), &record.StatusInfo); err != nil {
		return record, err
	}
	record.CreatedAt = time.UnixMilli(createdAt)
	record.UpdatedAt = time.UnixMilli(updatedAt)
	return record, nil
}
