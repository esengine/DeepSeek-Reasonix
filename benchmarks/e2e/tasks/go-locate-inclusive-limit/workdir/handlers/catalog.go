package handlers

import (
	"fmt"

	"example.com/svc/store"
	"example.com/svc/validate"
)

// CreateCatalog validates an order for the catalog flow and stores it.
func CreateCatalog(o store.Order) error {
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
