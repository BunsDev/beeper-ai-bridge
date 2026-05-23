package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

type SQLiteSessionCreateOptions struct {
	ID                string
	Cwd               string
	ParentSessionPath string
}

type SQLiteSessionListOptions struct {
	Cwd string
}

type SQLiteSessionForkOptions struct {
	SQLiteSessionCreateOptions
	EntryID  string
	Position string
}

type SessionRepo interface {
	Create(context.Context, SQLiteSessionCreateOptions) (*Session, error)
	Open(context.Context, SQLiteSessionMetadata) (*Session, error)
	List(context.Context, SQLiteSessionListOptions) ([]SQLiteSessionMetadata, error)
	Delete(context.Context, SQLiteSessionMetadata) error
	Fork(context.Context, SQLiteSessionMetadata, SQLiteSessionForkOptions) (*Session, error)
}

type SQLiteSessionRepo struct {
	Path string
}

var _ SessionRepo = (*SQLiteSessionRepo)(nil)

func NewSQLiteSessionRepo(path string) *SQLiteSessionRepo {
	return &SQLiteSessionRepo{Path: path}
}

func (r *SQLiteSessionRepo) Create(ctx context.Context, options SQLiteSessionCreateOptions) (*Session, error) {
	storage, err := CreateSQLiteSessionStorage(ctx, r.Path, options.Cwd, options.ID, options.ParentSessionPath)
	if err != nil {
		return nil, err
	}
	return NewSession(storage), nil
}

func (r *SQLiteSessionRepo) Open(ctx context.Context, metadata SQLiteSessionMetadata) (*Session, error) {
	path := metadata.Path
	if path == "" {
		path = r.Path
	}
	storage, err := OpenSQLiteSessionStorage(ctx, path, metadata.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NewSessionError(SessionErrorNotFound, "Session not found: "+metadata.ID, nil)
		}
		return nil, err
	}
	return NewSession(storage), nil
}

func (r *SQLiteSessionRepo) List(ctx context.Context, options SQLiteSessionListOptions) ([]SQLiteSessionMetadata, error) {
	db, err := openMigratedDB(ctx, r.Path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `select id, created_at, cwd, path, coalesce(parent_session_path, '') from sessions`
	args := []any{}
	if options.Cwd != "" {
		query += ` where cwd = ?`
		args = append(args, options.Cwd)
	}
	query += ` order by created_at desc`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SQLiteSessionMetadata
	for rows.Next() {
		var metadata SQLiteSessionMetadata
		if err := rows.Scan(&metadata.ID, &metadata.CreatedAt, &metadata.Cwd, &metadata.Path, &metadata.ParentSessionPath); err != nil {
			return nil, err
		}
		sessions = append(sessions, metadata)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt > sessions[j].CreatedAt
	})
	return sessions, nil
}

func (r *SQLiteSessionRepo) Delete(ctx context.Context, metadata SQLiteSessionMetadata) error {
	path := metadata.Path
	if path == "" {
		path = r.Path
	}
	db, err := openMigratedDB(ctx, path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from entries where session_id = ?`, metadata.ID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from sessions where id = ?`, metadata.ID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *SQLiteSessionRepo) Fork(ctx context.Context, sourceMetadata SQLiteSessionMetadata, options SQLiteSessionForkOptions) (*Session, error) {
	sourceSession, err := r.Open(ctx, sourceMetadata)
	if err != nil {
		return nil, err
	}
	sourceStorage, ok := sourceSession.GetStorage().(*SQLiteSessionStorage)
	if !ok {
		return nil, NewSessionError(SessionErrorInvalidSession, "source session storage is not SQLiteSessionStorage", nil)
	}
	defer sourceStorage.Close()

	entries, err := entriesToFork(ctx, sourceStorage, options.EntryID, options.Position)
	if err != nil {
		return nil, err
	}
	parentSessionPath := options.ParentSessionPath
	if parentSessionPath == "" {
		parentSessionPath = sourceMetadata.Path
	}
	targetStorage, err := CreateSQLiteSessionStorage(ctx, r.Path, options.Cwd, options.ID, parentSessionPath)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if _, err := targetStorage.AppendEntry(ctx, entry); err != nil {
			_ = targetStorage.Close()
			return nil, err
		}
	}
	return NewSession(targetStorage), nil
}

func openMigratedDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	storage := &SQLiteSessionStorage{db: db}
	if err := storage.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func entriesToFork(ctx context.Context, storage SessionStorage, entryID string, position string) ([]json.RawMessage, error) {
	if entryID == "" {
		return storage.GetEntries(ctx)
	}
	targetRaw, err := storage.GetEntry(ctx, entryID)
	if err != nil {
		if errors.Is(err, ErrSessionEntryNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, NewSessionError(SessionErrorInvalidForkTarget, "Entry "+entryID+" not found", err)
		}
		return nil, err
	}
	var target map[string]any
	if err := json.Unmarshal(targetRaw, &target); err != nil {
		return nil, err
	}
	effectiveLeafID := entryID
	if position == "" || position == "before" {
		if target["type"] != "message" {
			return nil, NewSessionError(SessionErrorInvalidForkTarget, "fork target is not a user message", nil)
		}
		rawMessage, err := json.Marshal(target["message"])
		if err != nil {
			return nil, err
		}
		var message struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(rawMessage, &message); err != nil {
			return nil, err
		}
		if message.Role != "user" {
			return nil, NewSessionError(SessionErrorInvalidForkTarget, "Entry "+entryID+" is not a user message", nil)
		}
		parentID, _ := target["parentId"].(string)
		if parentID == "" {
			return storage.GetPathToRoot(ctx, nil)
		}
		effectiveLeafID = parentID
	}
	return storage.GetPathToRoot(ctx, &effectiveLeafID)
}
