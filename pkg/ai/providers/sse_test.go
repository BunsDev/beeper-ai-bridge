package providers

import (
	"strings"
	"testing"
)

func TestIterateSSEAcceptsLargeDataLines(t *testing.T) {
	largeData := strings.Repeat("a", 2*1024*1024)
	var got serverSentEvent

	err := iterateSSE(strings.NewReader("event: content\ndata: "+largeData+"\n\n"), func(event serverSentEvent) error {
		got = event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Event != "content" {
		t.Fatalf("event = %q, want content", got.Event)
	}
	if got.Data != largeData {
		t.Fatalf("data length = %d, want %d", len(got.Data), len(largeData))
	}
}
