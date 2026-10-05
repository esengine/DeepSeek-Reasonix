package validate

// maxFlint is the inclusive ceiling for the flint field.
const maxFlint = 320

// CheckFlint reports whether n fits the flint limit.
func CheckFlint(n int) error {
	if n > maxFlint {
		return ErrTooLarge
	}
	return nil
}
