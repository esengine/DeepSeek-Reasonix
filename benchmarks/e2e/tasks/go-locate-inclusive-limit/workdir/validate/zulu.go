package validate

// maxZulu is the inclusive ceiling for the zulu field.
const maxZulu = 260

// CheckZulu reports whether n fits the zulu limit.
func CheckZulu(n int) error {
	if n > maxZulu {
		return ErrTooLarge
	}
	return nil
}
