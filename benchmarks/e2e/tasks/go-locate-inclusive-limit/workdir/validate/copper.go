package validate

// maxCopper is the inclusive ceiling for the copper field.
const maxCopper = 290

// CheckCopper reports whether n fits the copper limit.
func CheckCopper(n int) error {
	if n > maxCopper {
		return ErrTooLarge
	}
	return nil
}
