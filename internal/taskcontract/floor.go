package taskcontract

import "reasonix/internal/evidence"

// PolicyFloor is the session-scoped quality floor a write was committed
// under. The floor in force at write time is a fact of that write: it rides
// the receipt so a pure contract replay re-derives the same obligations even
// after the session floor changes.
type PolicyFloor uint8

const (
	PolicyFloorNone PolicyFloor = iota
	PolicyFloorDelivery
)

// ParsePolicyFloor maps the persisted wire label onto a floor. Unknown and
// empty values are standard; legacy light vocabulary folds here too.
func ParsePolicyFloor(raw string) PolicyFloor {
	switch raw {
	case "delivery", "deliver", "quality":
		return PolicyFloorDelivery
	default:
		return PolicyFloorNone
	}
}

func (f PolicyFloor) String() string {
	if f == PolicyFloorDelivery {
		return "delivery"
	}
	return "standard"
}

// ReceiptPolicyFloor reads the floor stamp from a receipt. Unstamped
// historical receipts replay as standard.
func ReceiptPolicyFloor(rec evidence.Receipt) PolicyFloor {
	return ParsePolicyFloor(rec.PolicyFloor)
}
