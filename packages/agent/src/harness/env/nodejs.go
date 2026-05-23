package env

import harness "github.com/earendil-works/pi-mono/packages/agent/src/harness"

type NodeExecutionEnv = harness.LocalExecutionEnv

type NodeExecutionEnvOptions struct {
	Cwd       string
	ShellPath string
	ShellEnv  map[string]string
}

func NewNodeExecutionEnv(options NodeExecutionEnvOptions) *NodeExecutionEnv {
	return &NodeExecutionEnv{Cwd: options.Cwd, ShellPath: options.ShellPath, ShellEnv: options.ShellEnv}
}
