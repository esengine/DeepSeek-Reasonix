package validate

// maxLotus is the inclusive ceiling for the lotus field.
const maxLotus = 380

// CheckLotus reports whether n fits the lotus limit.
func CheckLotus(n int) error {
	if n > maxLotus {
		return ErrTooLarge
	}
	return nil
}
