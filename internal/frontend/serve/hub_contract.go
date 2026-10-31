package serve

import "reasonix/internal/session/team"

// OpenRequest creates a fresh runtime or focuses the owner of SessionPath.
type OpenRequest struct {
	AgentComposition string `json:"agentComposition,omitempty"`
	Root             string `json:"root"`
	SessionPath      string `json:"sessionPath"`
	Model            string `json:"model"`
	// team seats the runtime on the user's team. Only the hub sets it; a
	// client's request body cannot.
	team *teamOpen
}

// teamOpen is the seat a runtime is opened for and the approval mode its agent
// was given, empty to follow the window.
type teamOpen struct {
	seat team.SeatRef
	mode string
}

type RuntimeView struct {
	AgentComposition *compositionSummary `json:"agentComposition,omitempty"`
	ID               string              `json:"id"`
	Base             string              `json:"base"`
	Root             string              `json:"root"`
	Name             string              `json:"name"`
	SessionPath      string              `json:"sessionPath,omitempty"`
	Host             string              `json:"host,omitempty"`
	ReadOnly         bool                `json:"readOnly,omitempty"`
	Team             *runtimeSeat        `json:"team,omitempty"`
}
