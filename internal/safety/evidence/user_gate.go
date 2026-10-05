package evidence

import (
	"encoding/json"
	"slices"
	"strings"
)

// A user gate declares that a turn's open list cannot advance without the user.
// It is read off the receipt: a gated turn and one that stopped early have the
// same shape.

// UserGateTool is the call that records a gate.
const UserGateTool = "await_user"

// UserGate is a recorded gate: the waiting item, and what the user must supply.
type UserGate struct {
	StepID string
	Need   string
}

// UserGateThisTurn returns the gate recorded this turn. Only a successful call
// counts; a refused one leaves readiness unchanged.
func (l *Ledger) UserGateThisTurn() (UserGate, bool) {
	if l == nil {
		return UserGate{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range slices.Backward(l.receipts) {
		if !r.Success || r.ToolName != UserGateTool {
			continue
		}
		return decodeUserGate(r.Args), true
	}
	return UserGate{}, false
}

func decodeUserGate(args json.RawMessage) UserGate {
	var payload struct {
		StepID string `json:"step_id"`
		Need   string `json:"need"`
	}
	if len(args) == 0 || json.Unmarshal(args, &payload) != nil {
		return UserGate{}
	}
	return UserGate{StepID: strings.TrimSpace(payload.StepID), Need: strings.TrimSpace(payload.Need)}
}
