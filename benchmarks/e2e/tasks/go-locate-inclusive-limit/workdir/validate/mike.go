package validate

// maxMike is the inclusive ceiling for the mike field.
const maxMike = 130

// CheckMike reports whether n fits the mike limit.
func CheckMike(n int) error {
	if n > maxMike {
		return ErrTooLarge
	}
	return nil
}
