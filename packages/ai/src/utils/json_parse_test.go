package utils

import "testing"

func TestRepairJSONEscapesInvalidStringContent(t *testing.T) {
	got := RepairJSON("{\"path\":\"C:\\z\\q\nnext\"}")
	want := "{\"path\":\"C:\\\\z\\\\q\\nnext\"}"
	if got != want {
		t.Fatalf("unexpected repair\n got: %q\nwant: %q", got, want)
	}
}

func TestParseJSONWithRepair(t *testing.T) {
	got, err := ParseJSONWithRepair[map[string]any]("{\"path\":\"C:\\z\"}")
	if err != nil {
		t.Fatal(err)
	}
	if got["path"] != "C:\\z" {
		t.Fatalf("unexpected parsed value %#v", got)
	}
}

func TestParseStreamingJSONReturnsCompletedMembers(t *testing.T) {
	got := ParseStreamingJSON(`{"path":"README.md","line":12,"unfinished"`)
	if got["path"] != "README.md" || got["line"] != float64(12) {
		t.Fatalf("expected completed members, got %#v", got)
	}
}

func TestParseStreamingJSONRepairsOnlyByOmittingIncompleteMember(t *testing.T) {
	got := ParseStreamingJSON(`{"path":"README.md","nested":{"ok":true},"partial":{"x"`)
	if got["path"] != "README.md" {
		t.Fatalf("expected path member, got %#v", got)
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok || nested["ok"] != true {
		t.Fatalf("expected nested member, got %#v", got)
	}
	if _, ok := got["partial"]; ok {
		t.Fatalf("expected incomplete member omitted, got %#v", got)
	}
}

func TestParseStreamingJSONCompleteObject(t *testing.T) {
	got := ParseStreamingJSON(`{"path":"README.md"}`)
	if got["path"] != "README.md" {
		t.Fatalf("expected complete object, got %#v", got)
	}
}
