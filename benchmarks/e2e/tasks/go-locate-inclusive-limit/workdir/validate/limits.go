package validate

import "errors"

// ErrTooLarge is returned by every Check* helper when a field exceeds the
// limit it guards. Limits are inclusive: a value equal to the limit is legal.
var ErrTooLarge = errors.New("validate: value over limit")
