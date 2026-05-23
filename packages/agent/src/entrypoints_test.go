package agent_test

import (
	"testing"

	harnesscompaction "github.com/earendil-works/pi-mono/packages/agent/src/harness/compaction"
	harnessenv "github.com/earendil-works/pi-mono/packages/agent/src/harness/env"
	harnessutils "github.com/earendil-works/pi-mono/packages/agent/src/harness/utils"
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
