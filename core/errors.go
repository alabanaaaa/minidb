package core

import "fmt"

// ErrorCode represents a categorized error for better error handling
type ErrorCode string

const (
	ErrCodeInvalidStock      ErrorCode = "invalid_stock"
	ErrCodeInvalidSale       ErrorCode = "invalid_sale"
	ErrCodeInvalidReconcile  ErrorCode = "invalid_reconcile"
	ErrCodeInsufficientStock ErrorCode = "insufficient_stock"
	ErrCodeNegativeValue     ErrorCode = "negative_value"
	ErrCodePersistence       ErrorCode = "persistence_error"
	ErrCodeNotFound          ErrorCode = "not_found"
	ErrCodeInvalidOperation  ErrorCode = "invalid_operation"
)

// DomainError represents an error with a code and message
type DomainError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

// NewDomainError creates a typed domain error
func NewDomainError(code ErrorCode, message string) *DomainError {
	return &DomainError{
		Code:    code,
		Message: message,
		Err:     nil,
	}
}

// NewDomainErrorWithCause wraps an underlying error
func NewDomainErrorWithCause(code ErrorCode, message string, err error) *DomainError {
	return &DomainError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// ValidationError represents validation failures with field-level details
type ValidationError struct {
	Field  string
	Reason string
	Value  interface{}
}

func (ve *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field %q: %s (value: %v)", ve.Field, ve.Reason, ve.Value)
}

// ValidationErrors collects multiple validation errors
type ValidationErrors []*ValidationError

func (ves ValidationErrors) Error() string {
	if len(ves) == 0 {
		return "no validation errors"
	}
	msg := fmt.Sprintf("%d validation error(s):\n", len(ves))
	for i, ve := range ves {
		msg += fmt.Sprintf("  %d. %s\n", i+1, ve.Error())
	}
	return msg
}

func (ves ValidationErrors) IsEmpty() bool {
	return len(ves) == 0
}
