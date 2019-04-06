package loadtest

import "fmt"

// ErrorCode allows us to encapsulate specific failure codes for the node
// processes.
type ErrorCode int

// Error/exit codes for load testing-related errors.
const (
	NoError ErrorCode = iota
	ErrFailedToDecodeConfig
	ErrFailedToReadConfigFile
	ErrInvalidConfig
	ErrFailedToCreateActor
	ErrTimedOutWaitingForSlaves
	ErrSlaveFailed
	ErrMasterFailed
	ErrKilled
	ErrStatsSanityCheckFailed
)

// Error is a way of wrapping the meaningful exit code we want to provide on
// failure.
type Error struct {
	Code     ErrorCode
	Message  string
	Upstream error
}

// Error implements error.
var _ error = (*Error)(nil)

// NewError allows us to create new Error structures from the given code and
// upstream error (can be nil).
func NewError(code ErrorCode, upstream error, additionalInfo ...string) *Error {
	return &Error{
		Code:     code,
		Message:  ErrorMessageForCode(code, additionalInfo...),
		Upstream: upstream,
	}
}

// Error implements error.
func (e Error) Error() string {
	if e.Upstream != nil {
		return fmt.Sprintf("%s. Caused by: %s", e.Message, e.Upstream.Error())
	}
	return e.Message
}

// ErrorMessageForCode translates the given error code into a human-readable,
// English message.
func ErrorMessageForCode(code ErrorCode, additionalInfo ...string) string {
	var result string
	switch code {
	case NoError:
		result = "No error"
	case ErrFailedToDecodeConfig:
		result = "Failed to decode TOML configuration"
	case ErrFailedToReadConfigFile:
		result = "Failed to read configuration file"
	case ErrInvalidConfig:
		result = "Invalid configuration"
	case ErrFailedToCreateActor:
		result = "Failed to create actor"
	case ErrTimedOutWaitingForSlaves:
		result = "Timed out waiting for slaves to connect"
	case ErrSlaveFailed:
		result = "Slave node failed"
	case ErrMasterFailed:
		result = "Master node failed"
	case ErrKilled:
		result = "Process killed"
	case ErrStatsSanityCheckFailed:
		result = "Statistics sanity check failed"
	default:
		return "Unrecognized error"
	}
	if len(additionalInfo) > 0 {
		result = fmt.Sprintf("%s: %s", result, additionalInfo[0])
	}
	return result
}

// IsErrorCode is a convenience function that attempts to cast the given error
// to an Error struct and checks its error code against the given code.
func IsErrorCode(err error, code ErrorCode) bool {
	e, ok := err.(Error)
	if ok {
		return e.Code == code
	}
	return false
}
