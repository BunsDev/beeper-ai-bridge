package session

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session/upgrades"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/util/dbutil"
	_ "go.mau.fi/util/dbutil/litestream"
)

type SQLiteSessionStorage struct {
	db       *dbutil.Database
	metadata SQLiteSessionMetadata
}

func OpenSQLiteSessionStorage(ctx context.Context, path string, sessionID string) (*SQLiteSessionStorage, error) {
	db, err := openSQLiteSessionDB(path)
	if err != nil {
		return nil, err
	}
	storage := &SQLiteSessionStorage{db: db}
	if err := migrateSQLiteSessionDB(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	row := db.QueryRow(ctx, `select id, created_at, cwd, path, coalesce(parent_session_path, '') from sessions where id = ?`, sessionID)
	if err := row.Scan(&storage.metadata.ID, &storage.metadata.CreatedAt, &storage.metadata.Cwd, &storage.metadata.Path, &storage.metadata.ParentSessionPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return storage, nil
}

func CreateSQLiteSessionStorage(ctx context.Context, path string, cwd string, sessionID string, parentSessionPath string) (*SQLiteSessionStorage, error) {
	if sessionID == "" {
		sessionID = CreateSessionID()
	}
	db, err := openSQLiteSessionDB(path)
	if err != nil {
		return nil, err
	}
	storage := &SQLiteSessionStorage{db: db, metadata: SQLiteSessionMetadata{SessionMetadata: SessionMetadata{ID: sessionID, CreatedAt: CreateTimestamp()}, Cwd: cwd, Path: path, ParentSessionPath: parentSessionPath}}
	if err := migrateSQLiteSessionDB(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, err = db.Exec(ctx, `insert into sessions (id, created_at, cwd, path, parent_session_path, leaf_id) values (?, ?, ?, ?, ?, null)`, storage.metadata.ID, storage.metadata.CreatedAt, cwd, path, nullString(parentSessionPath))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return storage, nil
}

func (s *SQLiteSessionStorage) Close() error {
	return s.db.Close()
}

func openSQLiteSessionDB(path string) (*dbutil.Database, error) {
	rawDB, err := sql.Open("sqlite3-fk-wal", path)
	if err != nil {
		return nil, err
	}
	db, err := dbutil.NewWithDB(rawDB, "sqlite3-fk-wal")
	if err != nil {
		_ = rawDB.Close()
		return nil, err
	}
	db.UpgradeTable = upgrades.Table
	db.VersionTable = "session_version"
	return db, nil
}

func migrateSQLiteSessionDB(ctx context.Context, db *dbutil.Database) error {
	return db.Upgrade(ctx)
}

func (s *SQLiteSessionStorage) GetMetadata(context.Context) (SQLiteSessionMetadata, error) {
	return s.metadata, nil
}

func (s *SQLiteSessionStorage) GetLeafID(ctx context.Context) (*string, error) {
	var leaf sql.NullString
	if err := s.db.QueryRow(ctx, `select leaf_id from sessions where id = ?`, s.metadata.ID).Scan(&leaf); err != nil {
		return nil, err
	}
	if !leaf.Valid {
		return nil, nil
	}
	var exists int
	if err := s.db.QueryRow(ctx, `select count(*) from entries where session_id = ? and id = ?`, s.metadata.ID, leaf.String).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, NewSessionError(SessionErrorInvalidSession, "Entry "+leaf.String+" not found", nil)
	}
	return &leaf.String, nil
}

func (s *SQLiteSessionStorage) SetLeafID(ctx context.Context, leafID *string) error {
	if leafID != nil {
		var exists int
		if err := s.db.QueryRow(ctx, `select count(*) from entries where session_id = ? and id = ?`, s.metadata.ID, *leafID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return NewSessionError(SessionErrorNotFound, "Entry "+*leafID+" not found", nil)
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
		"timestamp": CreateTimestamp(),
		"targetId":  leafID,
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `insert into entries (session_id, id, parent_id, type, timestamp, data) values (?, ?, ?, ?, ?, ?)`, s.metadata.ID, entryID, currentLeafID, "leaf", entry["timestamp"], string(raw)); err != nil {
			return err
		}
		if _, err := s.db.Exec(ctx, `update sessions set leaf_id = ? where id = ?`, leafID, s.metadata.ID); err != nil {
			return err
		}
		return nil
	})
}

func (s *SQLiteSessionStorage) CreateEntryID(ctx context.Context) (string, error) {
	for i := 0; i < 100; i++ {
		id := CreateSessionID()[:8]
		var exists int
		if err := s.db.QueryRow(ctx, `select count(*) from entries where session_id = ? and id = ?`, s.metadata.ID, id).Scan(&exists); err != nil {
			return "", err
		}
		if exists == 0 {
			return id, nil
		}
	}
	return CreateSessionID(), nil
}

func (s *SQLiteSessionStorage) AppendEntry(ctx context.Context, raw json.RawMessage) (string, error) {
	var entry SessionTreeEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", err
	}
	if entry.ID == "" || entry.Type == "" || entry.Timestamp == "" {
		return "", NewSessionError(SessionErrorInvalidEntry, "invalid session entry", nil)
	}
	if err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `insert into entries (session_id, id, parent_id, type, timestamp, data) values (?, ?, ?, ?, ?, ?)`, s.metadata.ID, entry.ID, entry.ParentID, entry.Type, entry.Timestamp, string(raw)); err != nil {
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
		if _, err := s.db.Exec(ctx, `update sessions set leaf_id = ? where id = ?`, nextLeafID, s.metadata.ID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", err
	}
	return entry.ID, nil
}

func (s *SQLiteSessionStorage) GetEntry(ctx context.Context, id string) (json.RawMessage, error) {
	var raw string
	err := s.db.QueryRow(ctx, `select data from entries where session_id = ? and id = ?`, s.metadata.ID, id).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSessionEntryNotFound
		}
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (s *SQLiteSessionStorage) FindEntries(ctx context.Context, entryType string) ([]json.RawMessage, error) {
	rows, err := s.db.Query(ctx, `select data from entries where session_id = ? and type = ? order by rowid`, s.metadata.ID, entryType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

func (s *SQLiteSessionStorage) GetLabel(ctx context.Context, id string) (*string, error) {
	labels, err := s.FindEntries(ctx, "label")
	if err != nil {
		return nil, err
	}
	var current *string
	for _, raw := range labels {
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		if entry["targetId"] != id {
			continue
		}
		label, ok := entry["label"].(string)
		if ok && trimSpace(label) != "" {
			trimmed := trimSpace(label)
			current = &trimmed
		} else {
			current = nil
		}
	}
	return current, nil
}

func (s *SQLiteSessionStorage) GetEntries(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.db.Query(ctx, `select data from entries where session_id = ? order by rowid`, s.metadata.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

func (s *SQLiteSessionStorage) GetPathToRoot(ctx context.Context, leafID *string) ([]json.RawMessage, error) {
	if leafID == nil {
		return []json.RawMessage{}, nil
	}
	byID := map[string]SessionTreeEntry{}
	rawByID := map[string]json.RawMessage{}
	entries, err := s.GetEntries(ctx)
	if err != nil {
		return nil, err
	}
	for _, raw := range entries {
		var entry SessionTreeEntry
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
			if currentID == *leafID {
				return nil, NewSessionError(SessionErrorNotFound, "Entry "+currentID+" not found", nil)
			}
			return nil, NewSessionError(SessionErrorInvalidSession, "Entry "+currentID+" not found", nil)
		}
		path = append([]json.RawMessage{rawByID[currentID]}, path...)
		if entry.ParentID == nil {
			break
		}
		currentID = *entry.ParentID
	}
	return path, nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
