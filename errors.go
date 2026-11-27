package gouci

import (
	"fmt"

	"github.com/honeybbq/gouci/internal/errs"
)

var (
	// ErrNotFound identifies a missing package, section, or option.
	ErrNotFound = errs.ErrNotFound
	// ErrInvalidSyntax identifies a UCI config or delta syntax error.
	ErrInvalidSyntax = errs.ErrInvalidSyntax
	// ErrInvalidName identifies an invalid package name, section name, section type, or option key.
	ErrInvalidName = errs.ErrInvalidName
	// ErrInvalidValue identifies a value containing characters rejected by libuci
	// (control bytes below 0x20 other than tab, LF, and CR).
	ErrInvalidValue = errs.ErrInvalidValue
	// ErrDuplicate identifies an attempt to create or rename a section or option to an already existing name.
	ErrDuplicate = errs.ErrDuplicate
	// ErrDirtyDelta identifies uncommitted changes in the delta directory under DeltaReject policy.
	ErrDirtyDelta = errs.ErrDirtyDelta
	// ErrReadOnly identifies a write attempt against a read-only Store.
	ErrReadOnly = errs.ErrReadOnly
)

// ParseError describes a syntax error encountered while parsing a UCI file or stream.
type ParseError struct {
	// File is the source filename or package name, when known.
	File string
	// Reason describes the syntax failure.
	Reason string
	// Line is the 1-based line number where the error occurred.
	Line int
	// Byte is the 0-based byte offset within Line where the error occurred.
	Byte int
}

// Error formats the parse error with its source location and reason.
func (e *ParseError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("uci parse %s:%d:%d: %s", e.File, e.Line, e.Byte, e.Reason)
	}

	return fmt.Sprintf("uci parse line %d, byte %d: %s", e.Line, e.Byte, e.Reason)
}

// Unwrap returns ErrInvalidSyntax so callers can match syntax failures with errors.Is.
func (e *ParseError) Unwrap() error {
	return ErrInvalidSyntax
}
