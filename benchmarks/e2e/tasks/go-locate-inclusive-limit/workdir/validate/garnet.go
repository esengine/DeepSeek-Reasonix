package validate

// maxGarnet is the inclusive ceiling for the garnet field.
const maxGarnet = 330

// CheckGarnet reports whether n fits the garnet limit.
func CheckGarnet(n int) error {
	if n > maxGarnet {
		return ErrTooLarge
	}
	return nil
}
