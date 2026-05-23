package compaction

import (
	agent "github.com/earendil-works/pi-mono/packages/agent/src"
	harness "github.com/earendil-works/pi-mono/packages/agent/src/harness"
	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func CreateFileOps() FileOperations {
	return harness.CreateFileOps()
}

func ExtractFileOpsFromMessage(message agent.AgentMessage, fileOps FileOperations) {
	harness.ExtractFileOpsFromMessage(message, fileOps)
}

func ComputeFileLists(fileOps FileOperations) (readFiles []string, modifiedFiles []string) {
	return harness.ComputeFileLists(fileOps)
}

func FormatFileOperations(readFiles []string, modifiedFiles []string) string {
	return harness.FormatFileOperations(readFiles, modifiedFiles)
}

func SerializeConversation(messages []ai.Message) string {
	return harness.SerializeConversation(messages)
}
