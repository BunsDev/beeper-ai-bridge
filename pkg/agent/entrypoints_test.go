package agent_test

import (
	"testing"

	harnesscompaction "github.com/beeper/ai-bridge/pkg/agent/harness/compaction"
	harnessenv "github.com/beeper/ai-bridge/pkg/agent/harness/env"
	harnessutils "github.com/beeper/ai-bridge/pkg/agent/harness/utils"
)

func TestPathCompatibleHarnessEntrypoints(t *testing.T) {
	env := harnessenv.NewNodeExecutionEnv(harnessenv.NodeExecutionEnvOptions{Cwd: t.TempDir()})
	if env.Cwd == "" {
		t.Fatal("expected node execution env")
	}
	if harnessutils.FormatSize(1024) != "1.0KB" {
		t.Fatal("expected utils shim")
	}
	if !harnesscompaction.DefaultCompactionSettings.Enabled {
		t.Fatal("expected compaction shim")
	}
}
