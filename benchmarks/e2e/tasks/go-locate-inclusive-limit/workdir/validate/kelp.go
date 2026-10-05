package validate

// maxKelp is the inclusive ceiling for the kelp field.
const maxKelp = 370

// CheckKelp reports whether n fits the kelp limit.
func CheckKelp(n int) error {
	if n > maxKelp {
		return ErrTooLarge
	}
	return nil
}
