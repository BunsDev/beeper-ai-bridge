package main

import "testing"

func TestParseArgsAcceptsIncludeUnregisteredBeforeOrAfterOutput(t *testing.T) {
	for _, args := range [][]string{
		{"--include-unregistered", "/tmp/models.go"},
		{"/tmp/models.go", "--include-unregistered"},
	} {
		outputPath, includeUnregistered, err := parseArgs(args)
		if err != nil {
			t.Fatalf("parseArgs(%v): %v", args, err)
		}
		if outputPath != "/tmp/models.go" {
			t.Fatalf("parseArgs(%v) output = %q", args, outputPath)
		}
		if !includeUnregistered {
			t.Fatalf("parseArgs(%v) did not enable includeUnregistered", args)
		}
	}
}
