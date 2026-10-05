package validate

// maxMaple is the inclusive ceiling for the maple field.
const maxMaple = 390

// CheckMaple reports whether n fits the maple limit.
func CheckMaple(n int) error {
	if n > maxMaple {
		return ErrTooLarge
	}
	return nil
}
