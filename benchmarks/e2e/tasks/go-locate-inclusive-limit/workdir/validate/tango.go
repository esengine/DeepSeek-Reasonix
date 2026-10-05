package validate

// maxTango is the inclusive ceiling for the tango field.
const maxTango = 200

// CheckTango reports whether n fits the tango limit.
func CheckTango(n int) error {
	if n > maxTango {
		return ErrTooLarge
	}
	return nil
}
