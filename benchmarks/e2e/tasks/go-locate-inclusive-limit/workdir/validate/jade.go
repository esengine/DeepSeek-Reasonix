package validate

// maxJade is the inclusive ceiling for the jade field.
const maxJade = 360

// CheckJade reports whether n fits the jade limit.
func CheckJade(n int) error {
	if n > maxJade {
		return ErrTooLarge
	}
	return nil
}
