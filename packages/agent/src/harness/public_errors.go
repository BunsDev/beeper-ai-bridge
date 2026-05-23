package harness

import "errors"

type CompactionErrorCode string

const (
	CompactionErrorAborted             CompactionErrorCode = "aborted"
	CompactionErrorSummarizationFailed CompactionErrorCode = "summarization_failed"
	CompactionErrorInvalidSession      CompactionErrorCode = "invalid_session"
	CompactionErrorUnknown             CompactionErrorCode = "unknown"
)

type CompactionError struct {
	Code  CompactionErrorCode
	Cause error
}

func NewCompactionError(code CompactionErrorCode, message string, cause error) *CompactionError {
	if cause == nil {
		cause = errors.New(message)
	}
	return &CompactionError{Code: code, Cause: cause}
}

func (e *CompactionError) Error() string { return e.Cause.Error() }
func (e *CompactionError) Unwrap() error { return e.Cause }

type BranchSummaryErrorCode string

const (
	BranchSummaryErrorAborted             BranchSummaryErrorCode = "aborted"
	BranchSummaryErrorSummarizationFailed BranchSummaryErrorCode = "summarization_failed"
	BranchSummaryErrorInvalidSession      BranchSummaryErrorCode = "invalid_session"
)

type BranchSummaryError struct {
	Code  BranchSummaryErrorCode
	Cause error
}

func NewBranchSummaryError(code BranchSummaryErrorCode, message string, cause error) *BranchSummaryError {
	if cause == nil {
		cause = errors.New(message)
	}
	return &BranchSummaryError{Code: code, Cause: cause}
}

func (e *BranchSummaryError) Error() string { return e.Cause.Error() }
func (e *BranchSummaryError) Unwrap() error { return e.Cause }

type AgentHarnessErrorCode string

const (
	AgentHarnessErrorBusy            AgentHarnessErrorCode = "busy"
	AgentHarnessErrorInvalidState    AgentHarnessErrorCode = "invalid_state"
	AgentHarnessErrorInvalidArgument AgentHarnessErrorCode = "invalid_argument"
	AgentHarnessErrorSession         AgentHarnessErrorCode = "session"
	AgentHarnessErrorHook            AgentHarnessErrorCode = "hook"
	AgentHarnessErrorAuth            AgentHarnessErrorCode = "auth"
	AgentHarnessErrorCompaction      AgentHarnessErrorCode = "compaction"
	AgentHarnessErrorBranchSummary   AgentHarnessErrorCode = "branch_summary"
	AgentHarnessErrorUnknown         AgentHarnessErrorCode = "unknown"
)

type AgentHarnessError struct {
	Code  AgentHarnessErrorCode
	Cause error
}

func NewAgentHarnessError(code AgentHarnessErrorCode, message string, cause error) *AgentHarnessError {
	if cause == nil {
		cause = errors.New(message)
	}
	return &AgentHarnessError{Code: code, Cause: cause}
}

func (e *AgentHarnessError) Error() string { return e.Cause.Error() }
func (e *AgentHarnessError) Unwrap() error { return e.Cause }
