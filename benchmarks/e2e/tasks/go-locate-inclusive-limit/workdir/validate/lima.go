package validate

// maxLima is the inclusive ceiling for the lima field.
const maxLima = 120

// CheckLima reports whether n fits the lima limit.
func CheckLima(n int) error {
	if n > maxLima {
		return ErrTooLarge
	}
	return nil
}
