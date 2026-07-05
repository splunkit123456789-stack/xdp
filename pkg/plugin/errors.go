package plugin

import "fmt"

type ErrorCode string

const (
	ErrInvalidConfig   ErrorCode = "INVALID_CONFIG"
	ErrParseFailed     ErrorCode = "PARSE_FAILED"
	ErrTransformFailed ErrorCode = "TRANSFORM_FAILED"
	ErrEnrichFailed    ErrorCode = "ENRICH_FAILED"
	ErrRouteFailed     ErrorCode = "ROUTE_FAILED"
	ErrOutputFailed    ErrorCode = "OUTPUT_FAILED"
	ErrTimeout         ErrorCode = "TIMEOUT"
	ErrExternal        ErrorCode = "EXTERNAL_ERROR"
)

type PluginError struct {
	Code      ErrorCode
	Message   string
	Retryable bool
	Cause     error
	Details   map[string]any
}

func NewError(code ErrorCode, message string, retryable bool, cause error) *PluginError {
	return &PluginError{Code: code, Message: message, Retryable: retryable, Cause: cause}
}

func (e *PluginError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
}

func (e *PluginError) Unwrap() error {
	return e.Cause
}
