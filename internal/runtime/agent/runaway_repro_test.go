package agent

import (
	"context"
	"encoding/json"
)

// readProbe is a read-only tool taking a path, so its receipt is classified the
// way a real reader's is — Read=true with Paths set — which is what the progress
// tracker scores as new evidence.
type readProbe struct{}

func (readProbe) Name() string        { return "read_file" }
func (readProbe) Description() string { return "read a file" }
func (readProbe) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (readProbe) ReadOnly() bool { return true }
func (readProbe) Execute(context.Context, json.RawMessage) (string, error) {
	return "package main\n\nfunc main() {}\n", nil
}
