package validate

// maxOscar is the inclusive ceiling for the oscar field.
const maxOscar = 150

// CheckOscar reports whether n fits the oscar limit.
func CheckOscar(n int) error {
	if n > maxOscar {
		return ErrTooLarge
	}
	return nil
}
