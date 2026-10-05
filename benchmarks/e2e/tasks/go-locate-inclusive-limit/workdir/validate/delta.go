package validate

// maxDelta is the inclusive ceiling for the delta field.
const maxDelta = 40

// CheckDelta reports whether n fits the delta limit.
func CheckDelta(n int) error {
	if n > maxDelta {
		return ErrTooLarge
	}
	return nil
}
