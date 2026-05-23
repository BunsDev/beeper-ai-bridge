package utils

import (
	"fmt"
	"time"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

type DiagnosticErrorInfo struct {
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	Code    any    `json:"code,omitempty"`
}

type AssistantMessageDiagnostic struct {
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	Error     *DiagnosticErrorInfo   `json:"error,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

func FormatThrownValue(value any) string {
	if err, ok := value.(error); ok {
		if err.Error() != "" {
			return err.Error()
		}
		return fmt.Sprintf("%T", err)
	}
	if value == nil {
		return "<nil>"
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func ExtractDiagnosticError(err error) DiagnosticErrorInfo {
	if err == nil {
		return DiagnosticErrorInfo{Name: "ThrownValue", Message: FormatThrownValue(nil)}
	}
	return DiagnosticErrorInfo{Name: fmt.Sprintf("%T", err), Message: FormatThrownValue(err)}
}

func CreateAssistantMessageDiagnostic(diagnosticType string, err error, details map[string]interface{}) AssistantMessageDiagnostic {
	info := ExtractDiagnosticError(err)
	return AssistantMessageDiagnostic{Type: diagnosticType, Timestamp: time.Now().UnixMilli(), Error: &info, Details: details}
}

func AppendAssistantMessageDiagnostic(message *ai.Message, diagnostic AssistantMessageDiagnostic) {
	if message == nil {
		return
	}
	message.Diagnostics = append(message.Diagnostics, diagnostic)
}
