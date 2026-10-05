package validate

// maxRomeo is the inclusive ceiling for the romeo field.
const maxRomeo = 180

// CheckRomeo reports whether n fits the romeo limit.
func CheckRomeo(n int) error {
	if n > maxRomeo {
		return ErrTooLarge
	}
	return nil
}
