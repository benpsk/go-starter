package console

import (
	"errors"
	"fmt"
)

// UsageError marks a caller mistake — an unknown command, a bad flag, a missing
// requirement — as distinct from a command that ran and failed. The two get
// different exit codes so scripts and CI can tell them apart.
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string {
	return e.Message
}

func usagef(format string, a ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, a...)}
}

// IsUsageError reports whether err came from misuse rather than execution.
func IsUsageError(err error) bool {
	if err == nil {
		return false
	}
	var target *UsageError
	return errors.As(err, &target)
}
