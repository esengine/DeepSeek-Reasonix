package handlers

import (
	"fmt"

	"example.com/svc/store"
	"example.com/svc/validate"
)

// CreateOrder validates an order for the order flow and stores it.
func CreateOrder(o store.Order) error {
	if err := validate.CheckQuebec(o.Quantity); err != nil {
		return err
	}
	if err := validate.CheckDelta(o.Weight); err != nil {
		return err
	}
	if err := validate.CheckMaple(o.Priority); err != nil {
		return err
	}
	if err := store.Save(o); err != nil {
		return fmt.Errorf("validation failed")
	}
	return nil
}
