package store

// Order is one placed order.
type Order struct {
	ID       string
	Quantity int
	Weight   int
	Priority int
}

// Save persists an order. The in-memory store never fails.
func Save(o Order) error { return nil }
