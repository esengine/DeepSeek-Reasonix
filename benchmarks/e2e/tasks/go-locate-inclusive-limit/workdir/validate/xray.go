package validate

// maxXray is the inclusive ceiling for the xray field.
const maxXray = 240

// CheckXray reports whether n fits the xray limit.
func CheckXray(n int) error {
	if n > maxXray {
		return ErrTooLarge
	}
	return nil
}
