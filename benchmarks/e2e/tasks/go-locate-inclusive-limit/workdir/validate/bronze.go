package validate

// maxBronze is the inclusive ceiling for the bronze field.
const maxBronze = 280

// CheckBronze reports whether n fits the bronze limit.
func CheckBronze(n int) error {
	if n > maxBronze {
		return ErrTooLarge
	}
	return nil
}
