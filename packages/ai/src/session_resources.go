package ai

import (
	"errors"
	"sync"
)

type SessionResourceCleanup func(sessionID ...string) error

var sessionResourceCleanupsMu sync.Mutex
var sessionResourceCleanupNextID int
var sessionResourceCleanups []registeredSessionResourceCleanup

type registeredSessionResourceCleanup struct {
	id      int
	cleanup SessionResourceCleanup
}

func RegisterSessionResourceCleanup(cleanup SessionResourceCleanup) func() {
	sessionResourceCleanupsMu.Lock()
	sessionResourceCleanupNextID++
	id := sessionResourceCleanupNextID
	sessionResourceCleanups = append(sessionResourceCleanups, registeredSessionResourceCleanup{id: id, cleanup: cleanup})
	sessionResourceCleanupsMu.Unlock()
	return func() {
		sessionResourceCleanupsMu.Lock()
		defer sessionResourceCleanupsMu.Unlock()
		for i, candidate := range sessionResourceCleanups {
			if candidate.id == id {
				sessionResourceCleanups = append(sessionResourceCleanups[:i], sessionResourceCleanups[i+1:]...)
				return
			}
		}
	}
}

func CleanupSessionResources(sessionID ...string) error {
	sessionResourceCleanupsMu.Lock()
	cleanups := append([]registeredSessionResourceCleanup(nil), sessionResourceCleanups...)
	sessionResourceCleanupsMu.Unlock()

	var errs []error
	for _, registered := range cleanups {
		if err := registered.cleanup(sessionID...); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
