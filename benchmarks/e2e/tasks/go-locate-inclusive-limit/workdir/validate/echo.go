package validate

// maxEcho is the inclusive ceiling for the echo field.
const maxEcho = 50

// CheckEcho reports whether n fits the echo limit.
func CheckEcho(n int) error {
	if n > maxEcho {
		return ErrTooLarge
	}
	return nil
}
