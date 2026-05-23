package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func ToSession(storage SessionStorage) *Session {
	return NewSession(storage)
}

func GetEntryMaybe(ctx context.Context, storage SessionStorage, id string) (*json.RawMessage, error) {
	raw, err := storage.GetEntry(ctx, id)
	if err == nil {
		return &raw, nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrSessionEntryNotFound) {
		return nil, nil
	}
	return nil, err
}

func GetEntriesToFork(ctx context.Context, storage SessionStorage, entryID string, position string) ([]json.RawMessage, error) {
	return entriesToFork(ctx, storage, entryID, position)
}
