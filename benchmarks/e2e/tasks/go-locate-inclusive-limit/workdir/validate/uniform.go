package validate

// maxUniform is the inclusive ceiling for the uniform field.
const maxUniform = 210

// CheckUniform reports whether n fits the uniform limit.
func CheckUniform(n int) error {
	if n > maxUniform {
		return ErrTooLarge
	}
	return nil
}
