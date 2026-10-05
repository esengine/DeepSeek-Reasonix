package validate

// maxCharlie is the inclusive ceiling for the charlie field.
const maxCharlie = 30

// CheckCharlie reports whether n fits the charlie limit.
func CheckCharlie(n int) error {
	if n > maxCharlie {
		return ErrTooLarge
	}
	return nil
}
