package validate

// maxYankee is the inclusive ceiling for the yankee field.
const maxYankee = 250

// CheckYankee reports whether n fits the yankee limit.
func CheckYankee(n int) error {
	if n > maxYankee {
		return ErrTooLarge
	}
	return nil
}
