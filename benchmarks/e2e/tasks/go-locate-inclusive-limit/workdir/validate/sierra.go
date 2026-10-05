package validate

// maxSierra is the inclusive ceiling for the sierra field.
const maxSierra = 190

// CheckSierra reports whether n fits the sierra limit.
func CheckSierra(n int) error {
	if n > maxSierra {
		return ErrTooLarge
	}
	return nil
}
