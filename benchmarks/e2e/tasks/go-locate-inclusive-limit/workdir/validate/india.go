package validate

// maxIndia is the inclusive ceiling for the india field.
const maxIndia = 90

// CheckIndia reports whether n fits the india limit.
func CheckIndia(n int) error {
	if n > maxIndia {
		return ErrTooLarge
	}
	return nil
}
