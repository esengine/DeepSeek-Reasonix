package agent

import (
	"context"
	"encoding/json"
)

// outsideWriter stands in for a tool whose call succeeds and names the file it
// wrote, whatever that file is. It is the shape of a turn that put something on
// the desktop while its workspace was somewhere else.
type outsideWriter struct{}

func (outsideWriter) Name() string        { return "write_file" }
func (outsideWriter) Description() string { return "write a file" }
func (outsideWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (outsideWriter) ReadOnly() bool         { return false }
func (outsideWriter) WritesNamedPaths() bool { return true }
func (outsideWriter) Execute(context.Context, json.RawMessage) (string, error) {
	return "wrote", nil
}
