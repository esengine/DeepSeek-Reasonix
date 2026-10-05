package validate

// maxFoxtrot is the inclusive ceiling for the foxtrot field.
const maxFoxtrot = 60

// CheckFoxtrot reports whether n fits the foxtrot limit.
func CheckFoxtrot(n int) error {
	if n > maxFoxtrot {
		return ErrTooLarge
	}
	return nil
}
