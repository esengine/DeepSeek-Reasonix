package validate

// maxHotel is the inclusive ceiling for the hotel field.
const maxHotel = 80

// CheckHotel reports whether n fits the hotel limit.
func CheckHotel(n int) error {
	if n > maxHotel {
		return ErrTooLarge
	}
	return nil
}
