package validate

// maxAmber is the inclusive ceiling for the amber field.
const maxAmber = 270

// CheckAmber reports whether n fits the amber limit.
func CheckAmber(n int) error {
	if n > maxAmber {
		return ErrTooLarge
	}
	return nil
}
