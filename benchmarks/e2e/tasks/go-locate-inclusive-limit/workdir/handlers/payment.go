package handlers

import (
	"fmt"

	"example.com/svc/store"
	"example.com/svc/validate"
)

// CreatePayment validates an order for the payment flow and stores it.
func CreatePayment(o store.Order) error {
	if err := validate.CheckAlpha(o.Quantity); err != nil {
		return err
	}
	if err := validate.CheckBravo(o.Weight); err != nil {
		return err
	}
	if err := store.Save(o); err != nil {
		return fmt.Errorf("validation failed")
	}
	return nil
}
