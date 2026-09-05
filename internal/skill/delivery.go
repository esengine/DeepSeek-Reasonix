// delivery.go — what finishing means for a worker, over and above answering.
package skill

// DeliveryContract is what a worker owes its caller when it is done. One member
// today; the shape is the point, because "what counts as finished" is a property
// of the worker rather than of the button that started it.
type DeliveryContract struct {
	// ReviewReport is the typed verdict this worker must submit before it may
	// finish, empty when it owes none.
	ReviewReport string
}

// The typed verdicts a worker can owe.
const (
	ReviewReportReview   = "review"
	ReviewReportSecurity = "security"
)
