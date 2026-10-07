package domain

import (
	"errors"
	"fmt"
)

// ErrInvalid marks input that breaks a domain rule. Transport maps it to
// InvalidArgument.
var ErrInvalid = errors.New("invalid input")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
