package ai

import (
	"errors"
	"testing"
)

func TestSessionResourceCleanupRunsAndUnregisters(t *testing.T) {
	calls := []string{}
	unregister := RegisterSessionResourceCleanup(func(sessionID ...string) error {
		if len(sessionID) > 0 {
			calls = append(calls, sessionID[0])
		} else {
			calls = append(calls, "")
		}
		return nil
	})
	if err := CleanupSessionResources("session-1"); err != nil {
		t.Fatal(err)
	}
	unregister()
	if err := CleanupSessionResources("session-2"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "session-1" {
		t.Fatalf("unexpected cleanup calls %#v", calls)
	}
}

func TestSessionResourceCleanupJoinsErrors(t *testing.T) {
	firstErr := errors.New("first")
	secondErr := errors.New("second")
	unregisterFirst := RegisterSessionResourceCleanup(func(...string) error { return firstErr })
	unregisterSecond := RegisterSessionResourceCleanup(func(...string) error { return secondErr })
	defer unregisterFirst()
	defer unregisterSecond()

	err := CleanupSessionResources()
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("expected joined cleanup errors, got %v", err)
	}
}
