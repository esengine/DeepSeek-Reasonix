package validate

// maxEmber is the inclusive ceiling for the ember field.
const maxEmber = 310

// CheckEmber reports whether n fits the ember limit.
func CheckEmber(n int) error {
	if n > maxEmber {
		return ErrTooLarge
	}
	return nil
}
