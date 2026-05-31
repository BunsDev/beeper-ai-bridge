package connector

import (
	"testing"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"gopkg.in/yaml.v3"
)

func TestConfigDefaultsCompaction(t *testing.T) {
	var config Config
	config.ApplyDefaults()
	settings := config.Compaction.Settings()
	if settings != harness.DefaultCompactionSettings {
		t.Fatalf("compaction settings = %#v, want %#v", settings, harness.DefaultCompactionSettings)
	}
}

func TestConfigCanDisableAutoCompaction(t *testing.T) {
	var config Config
	if err := yaml.Unmarshal([]byte(`compaction:
  enabled: false
`), &config); err != nil {
		t.Fatal(err)
	}
	settings := config.Compaction.Settings()
	if settings.Enabled {
		t.Fatalf("expected compaction to be disabled: %#v", settings)
	}
	if settings.ReserveTokens != harness.DefaultCompactionSettings.ReserveTokens || settings.KeepRecentTokens != harness.DefaultCompactionSettings.KeepRecentTokens {
		t.Fatalf("disabled compaction lost token defaults: %#v", settings)
	}
}
