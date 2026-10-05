package validate

// maxNectar is the inclusive ceiling for the nectar field.
const maxNectar = 400

// CheckNectar reports whether n fits the nectar limit.
func CheckNectar(n int) error {
	if n > maxNectar {
		return ErrTooLarge
	}
	return nil
}
