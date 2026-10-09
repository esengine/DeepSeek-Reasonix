package provider

// Request is a single completion request.
type Request struct {
	Messages    []Message
	Tools       []ToolSchema
	Temperature *float64 // nil = omit; non-nil = send the value, including 0
	MaxTokens   int
	// ResponseFormat, when non-nil, asks the endpoint for structured JSON
	// output (Responses: text.format.type=json_object). Nil omits the field
	// entirely — the common path must stay byte-stable for prompt caching.
	ResponseFormat *ResponseFormat `json:"ResponseFormat,omitempty"`
	EffortOverride string          `json:"EffortOverride,omitempty"` // per-call reasoning-depth override; adapters apply it only when the endpoint's effort vocabulary accepts it
	// Mode is the session's model mode id. An adapter sends only a mode its
	// config declared for this endpoint, so an unknown one reaches no wire.
	Mode string `json:"Mode,omitempty"`
	// Summary marks the request that asks for a compaction briefing, so a
	// double or an adapter can tell it from a turn without reading its prose.
	Summary bool `json:"-"`
}

// ResponseFormat asks a provider to constrain its output shape.
type ResponseFormat struct {
	// Type is the structured format: "json_object" is the only shape the
	// Responses endpoints currently define (MiMo/DashScope/OpenAI).
	Type string `json:"type"`
}
