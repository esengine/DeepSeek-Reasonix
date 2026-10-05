package validate

// maxVictor is the inclusive ceiling for the victor field.
const maxVictor = 220

// CheckVictor reports whether n fits the victor limit.
func CheckVictor(n int) error {
	if n > maxVictor {
		return ErrTooLarge
	}
	return nil
}
