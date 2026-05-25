package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/ai/modelcatalog"
)

func main() {
	outputPath, includeUnregistered, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	catalog, err := modelcatalog.Build(context.Background(), modelcatalog.Options{
		IncludeUnregistered: includeUnregistered,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate models: %v\n", err)
		os.Exit(1)
	}
	source, err := modelcatalog.GenerateGoSource(catalog, "ai")
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate Go source: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputPath, source, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	if len(catalog.Skipped) > 0 && !includeUnregistered {
		fmt.Fprintf(os.Stderr, "Skipped providers with unregistered APIs: %s. Use --include-unregistered after porting providers.\n", providerList(catalog.Skipped))
	}
	fmt.Printf("Wrote %d models across %d providers to %s\n", catalog.Count(), len(catalog.ProviderOrder), outputPath)
}

func parseArgs(args []string) (string, bool, error) {
	outputPath := "pkg/ai/models_generated.go"
	includeUnregistered := false
	outputSet := false
	for _, arg := range args {
		switch arg {
		case "--include-unregistered":
			includeUnregistered = true
		case "-h", "--help":
			return "", false, fmt.Errorf("usage: generate-models-go [output-path] [--include-unregistered]")
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown flag %s", arg)
			}
			if outputSet {
				return "", false, fmt.Errorf("unexpected argument %s", arg)
			}
			outputPath = arg
			outputSet = true
		}
	}
	return outputPath, includeUnregistered, nil
}

func providerList(providers []ai.Provider) string {
	parts := make([]string, 0, len(providers))
	for _, provider := range providers {
		parts = append(parts, string(provider))
	}
	return strings.Join(parts, ", ")
}
