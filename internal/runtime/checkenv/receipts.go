package checkenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sync"

	"reasonix/internal/safety/evidence"
)

// A restart or eviction makes old observations unknown and requires a recheck.
// Persisted JSON, including a caller's Complete=true, cannot mint this authority.
var receipts = struct {
	sync.Mutex
	inputs map[string]ownedReceipt
	order  []string
}{inputs: map[string]ownedReceipt{}}

type ownedReceipt struct {
	input     evidence.CheckInputs
	execution string
	capture   string
}

func recordOwnedReceipt(input evidence.CheckInputs, execution, capture string) {
	receipts.Lock()
	defer receipts.Unlock()
	if len(receipts.order) >= 4096 {
		delete(receipts.inputs, receipts.order[0])
		receipts.order = receipts.order[1:]
	}
	receipts.inputs[input.EnvironmentReceipt] = ownedReceipt{input, execution, capture}
	receipts.order = append(receipts.order, input.EnvironmentReceipt)
}

func Verify(input evidence.CheckInputs, executionID string) bool {
	return verifyReceipt(input, executionID, "")
}

func verifyReceipt(input evidence.CheckInputs, executionID, capture string) bool {
	if input.EnvironmentReceipt == "" {
		return false
	}
	receipts.Lock()
	defer receipts.Unlock()
	owned, exists := receipts.inputs[input.EnvironmentReceipt]
	return exists && owned.execution == executionID && owned.capture == capture && reflect.DeepEqual(owned.input, input)
}

func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
