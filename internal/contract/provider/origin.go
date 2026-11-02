package provider

// Origin names who a turn the host ran was for, when it was not the user: an
// agent's message, its reply, or a group's turn. Declared by the host that
// queued the turn. Local UI metadata, stripped before every provider request.
type Origin struct {
	Kind     string `json:"kind"`
	Agent    string `json:"agent,omitempty"`
	Exchange string `json:"exchange,omitempty"`
	Group    string `json:"group,omitempty"`
}
