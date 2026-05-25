package aidb

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	"github.com/beeper/ai-bridge/pkg/aidb/upgrades"
	"go.mau.fi/util/dbutil"
)

type Store struct {
	db *dbutil.Database
}

func NewStore(db *dbutil.Database, log dbutil.DatabaseLogger) *Store {
	return &Store{db: db.Child("ai_bridge_version", upgrades.Table, log)}
}

func (s *Store) Upgrade(ctx context.Context) error {
	return s.db.Upgrade(ctx)
}

func (s *Store) CreateSession(ctx context.Context, options session.SQLiteSessionCreateOptions) (*session.Session, error) {
	storage, err := CreateSessionStorage(ctx, s.db, options)
	if err != nil {
		return nil, err
	}
	return session.NewSession(storage), nil
}

func (s *Store) OpenSession(ctx context.Context, metadata session.SQLiteSessionMetadata) (*session.Session, error) {
	storage, err := OpenSessionStorage(ctx, s.db, metadata.ID)
	if err != nil {
		return nil, err
	}
	return session.NewSession(storage), nil
}

type SessionStorage struct {
	db       *dbutil.Database
	metadata session.SQLiteSessionMetadata
}

var _ session.SessionStorage = (*SessionStorage)(nil)

func CreateSessionStorage(ctx context.Context, db *dbutil.Database, options session.SQLiteSessionCreateOptions) (*SessionStorage, error) {
	if options.ID == "" {
		options.ID = session.CreateSessionID()
	}
	storage := &SessionStorage{
		db: db,
		metadata: session.SQLiteSessionMetadata{
			SessionMetadata: session.SessionMetadata{
				ID:        options.ID,
				CreatedAt: session.CreateTimestamp(),
			},
			ParentSessionPath: options.ParentSessionPath,
		},
	}
	_, err := db.Exec(ctx, `
		INSERT INTO ai_session (id, created_at, parent_session_path, leaf_id)
		VALUES ($1, $2, $3, NULL)
	`, storage.metadata.ID, storage.metadata.CreatedAt, nullString(storage.metadata.ParentSessionPath))
	if err != nil {
		return nil, err
	}
	return storage, nil
}

func OpenSessionStorage(ctx context.Context, db *dbutil.Database, sessionID string) (*SessionStorage, error) {
	storage := &SessionStorage{db: db}
	row := db.QueryRow(ctx, `
		SELECT id, created_at, COALESCE(parent_session_path, '')
		FROM ai_session
		WHERE id=$1
	`, sessionID)
	if err := row.Scan(&storage.metadata.ID, &storage.metadata.CreatedAt, &storage.metadata.ParentSessionPath); err != nil {
		return nil, err
	}
	return storage, nil
}

func (s *SessionStorage) GetMetadata(context.Context) (session.SQLiteSessionMetadata, error) {
	return s.metadata, nil
}

func (s *SessionStorage) GetLeafID(ctx context.Context) (*string, error) {
	var leaf sql.NullString
	if err := s.db.QueryRow(ctx, `SELECT leaf_id FROM ai_session WHERE id=$1`, s.metadata.ID).Scan(&leaf); err != nil {
		return nil, err
	}
	if !leaf.Valid {
		return nil, nil
	}
	var exists int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM ai_session_entry WHERE session_id=$1 AND id=$2`, s.metadata.ID, leaf.String).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, session.NewSessionError(session.SessionErrorInvalidSession, "Entry "+leaf.String+" not found", nil)
	}
	return &leaf.String, nil
}

func (s *SessionStorage) SetLeafID(ctx context.Context, leafID *string) error {
	if leafID != nil {
		var exists int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM ai_session_entry WHERE session_id=$1 AND id=$2`, s.metadata.ID, *leafID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return session.NewSessionError(session.SessionErrorNotFound, "Entry "+*leafID+" not found", nil)
		}
	}
	currentLeafID, err := s.GetLeafID(ctx)
	if err != nil {
		return err
	}
	entryID, err := s.CreateEntryID(ctx)
	if err != nil {
		return err
	}
	entry := map[string]any{
		"type":      "leaf",
		"id":        entryID,
		"parentId":  currentLeafID,
		"timestamp": session.CreateTimestamp(),
		"targetId":  leafID,
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `
			INSERT INTO ai_session_entry (session_id, id, parent_id, type, timestamp, data)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, s.metadata.ID, entryID, currentLeafID, "leaf", entry["timestamp"], string(raw)); err != nil {
			return err
		}
		_, err := s.db.Exec(ctx, `UPDATE ai_session SET leaf_id=$1 WHERE id=$2`, leafID, s.metadata.ID)
		return err
	})
}

func (s *SessionStorage) CreateEntryID(ctx context.Context) (string, error) {
	for i := 0; i < 100; i++ {
		id := session.CreateSessionID()[:8]
		var exists int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM ai_session_entry WHERE session_id=$1 AND id=$2`, s.metadata.ID, id).Scan(&exists); err != nil {
			return "", err
		}
		if exists == 0 {
			return id, nil
		}
	}
	return session.CreateSessionID(), nil
}

func (s *SessionStorage) AppendEntry(ctx context.Context, raw json.RawMessage) (string, error) {
	var entry session.SessionTreeEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", err
	}
	if entry.ID == "" || entry.Type == "" || entry.Timestamp == "" {
		return "", session.NewSessionError(session.SessionErrorInvalidEntry, "invalid session entry", nil)
	}
	err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `
			INSERT INTO ai_session_entry (session_id, id, parent_id, type, timestamp, data)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, s.metadata.ID, entry.ID, entry.ParentID, entry.Type, entry.Timestamp, string(raw)); err != nil {
			return err
		}
		nextLeafID := &entry.ID
		if entry.Type == "leaf" {
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				return err
			}
			if targetID, ok := body["targetId"].(string); ok {
				nextLeafID = &targetID
			} else {
				nextLeafID = nil
			}
		}
		_, err := s.db.Exec(ctx, `UPDATE ai_session SET leaf_id=$1 WHERE id=$2`, nextLeafID, s.metadata.ID)
		return err
	})
	if err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (s *SessionStorage) GetEntry(ctx context.Context, id string) (json.RawMessage, error) {
	var raw string
	err := s.db.QueryRow(ctx, `SELECT data FROM ai_session_entry WHERE session_id=$1 AND id=$2`, s.metadata.ID, id).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, session.ErrSessionEntryNotFound
		}
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (s *SessionStorage) FindEntries(ctx context.Context, entryType string) ([]json.RawMessage, error) {
	rows, err := s.db.Query(ctx, `SELECT data FROM ai_session_entry WHERE session_id=$1 AND type=$2 ORDER BY rowid`, s.metadata.ID, entryType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawEntries(rows)
}

func (s *SessionStorage) GetLabel(ctx context.Context, id string) (*string, error) {
	entries, err := s.FindEntries(ctx, "label")
	if err != nil {
		return nil, err
	}
	var current *string
	for _, raw := range entries {
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		if entry["targetId"] != id {
			continue
		}
		label, ok := entry["label"].(string)
		if ok && label != "" {
			current = &label
		} else {
			current = nil
		}
	}
	return current, nil
}

func (s *SessionStorage) GetEntries(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.db.Query(ctx, `SELECT data FROM ai_session_entry WHERE session_id=$1 ORDER BY rowid`, s.metadata.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawEntries(rows)
}

func (s *SessionStorage) GetPathToRoot(ctx context.Context, leafID *string) ([]json.RawMessage, error) {
	if leafID == nil {
		return []json.RawMessage{}, nil
	}
	entries, err := s.GetEntries(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]session.SessionTreeEntry{}
	rawByID := map[string]json.RawMessage{}
	for _, raw := range entries {
		var entry session.SessionTreeEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		byID[entry.ID] = entry
		rawByID[entry.ID] = raw
	}
	var path []json.RawMessage
	currentID := *leafID
	for {
		entry, ok := byID[currentID]
		if !ok {
			return nil, session.NewSessionError(session.SessionErrorNotFound, "Entry "+currentID+" not found", nil)
		}
		path = append([]json.RawMessage{rawByID[currentID]}, path...)
		if entry.ParentID == nil {
			break
		}
		currentID = *entry.ParentID
	}
	return path, nil
}

type rawEntryRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanRawEntries(rows rawEntryRows) ([]json.RawMessage, error) {
	entries := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		entries = append(entries, json.RawMessage(raw))
	}
	return entries, rows.Err()
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
