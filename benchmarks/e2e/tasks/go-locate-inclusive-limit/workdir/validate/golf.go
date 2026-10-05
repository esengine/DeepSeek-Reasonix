package validate

// maxGolf is the inclusive ceiling for the golf field.
const maxGolf = 70

// CheckGolf reports whether n fits the golf limit.
func CheckGolf(n int) error {
	if n > maxGolf {
		return ErrTooLarge
	}
	return nil
}
