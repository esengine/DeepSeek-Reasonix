package validate

// maxWhiskey is the inclusive ceiling for the whiskey field.
const maxWhiskey = 230

// CheckWhiskey reports whether n fits the whiskey limit.
func CheckWhiskey(n int) error {
	if n > maxWhiskey {
		return ErrTooLarge
	}
	return nil
}
