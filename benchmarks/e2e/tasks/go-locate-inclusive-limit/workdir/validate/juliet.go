package validate

// maxJuliet is the inclusive ceiling for the juliet field.
const maxJuliet = 100

// CheckJuliet reports whether n fits the juliet limit.
func CheckJuliet(n int) error {
	if n > maxJuliet {
		return ErrTooLarge
	}
	return nil
}
