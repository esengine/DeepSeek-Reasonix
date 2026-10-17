package serve

// OpenRequest creates a fresh runtime or focuses the owner of SessionPath.
type OpenRequest struct {
	AgentComposition string `json:"agentComposition,omitempty"`
	Root             string `json:"root"`
	SessionPath      string `json:"sessionPath"`
	Model            string `json:"model"`
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
}
