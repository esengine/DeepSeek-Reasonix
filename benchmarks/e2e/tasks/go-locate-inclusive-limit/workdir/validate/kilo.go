package validate

// maxKilo is the inclusive ceiling for the kilo field.
const maxKilo = 110

// CheckKilo reports whether n fits the kilo limit.
func CheckKilo(n int) error {
	if n > maxKilo {
		return ErrTooLarge
	}
	return nil
}
