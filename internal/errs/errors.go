// Package errs owns shared sentinel errors and wrapping helpers for gouci.
package errs

import (
	"errors"
	"fmt"
)

// ErrNotFound and the other sentinels classify errors returned by gouci.
var (
	ErrNotFound      = errors.New("uci: not found")
	ErrInvalidSyntax = errors.New("uci: invalid syntax")
	ErrInvalidName   = errors.New("uci: invalid identifier")
	ErrInvalidValue  = errors.New("uci: invalid value")
	ErrDuplicate     = errors.New("uci: duplicate entry")
	ErrDirtyDelta    = errors.New("uci: uncommitted delta exists")
	ErrReadOnly      = errors.New("uci: store is read-only")
)

// Wrapf adds formatted context while preserving error identity.
func Wrapf(err error, format string, a ...any) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", fmt.Sprintf(format, a...), err)
}
