package validate

// maxPapa is the inclusive ceiling for the papa field.
const maxPapa = 160

// CheckPapa reports whether n fits the papa limit.
func CheckPapa(n int) error {
	if n > maxPapa {
		return ErrTooLarge
	}
	return nil
}
