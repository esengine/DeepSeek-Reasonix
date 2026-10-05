package validate

// maxDune is the inclusive ceiling for the dune field.
const maxDune = 300

// CheckDune reports whether n fits the dune limit.
func CheckDune(n int) error {
	if n > maxDune {
		return ErrTooLarge
	}
	return nil
}
