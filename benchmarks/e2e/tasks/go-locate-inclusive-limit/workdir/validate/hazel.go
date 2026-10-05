package validate

// maxHazel is the inclusive ceiling for the hazel field.
const maxHazel = 340

// CheckHazel reports whether n fits the hazel limit.
func CheckHazel(n int) error {
	if n > maxHazel {
		return ErrTooLarge
	}
	return nil
}
