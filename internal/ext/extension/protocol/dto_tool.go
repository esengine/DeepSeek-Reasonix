package protocol

import "encoding/json"

// Tool DTOs: a model-callable tool the extension declared in its manifest and
// activated at initialize. The host owns the tool's model-visible name, its
// permission check and its timeout; the extension only runs the call.

// ToolCallParams runs one call of a declared tool. Name is the tool as the
// manifest declares it, not the host-qualified name the model sees.
type ToolCallParams struct {
	Name          string          `json:"name" validate:"nonempty"`
	Arguments     json.RawMessage `json:"arguments"`
	TimeoutMillis int             `json:"timeoutMillis" validate:"min=0"`
}

// ToolCallResult is what the model reads as the tool's output. IsError marks a
// failure the tool reports about its own work, which the model can act on; a
// call the extension could not run at all is a protocol error instead.
type ToolCallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"isError,omitempty"`
}
