package validate

// maxIvory is the inclusive ceiling for the ivory field.
const maxIvory = 350

// CheckIvory reports whether n fits the ivory limit.
func CheckIvory(n int) error {
	if n > maxIvory {
		return ErrTooLarge
	}
	return nil
}
