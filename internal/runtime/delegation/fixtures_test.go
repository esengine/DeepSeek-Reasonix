package delegation

import (
	"context"
	"encoding/json"
)

type fakeTool struct {
	name     string
	readOnly bool
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) Description() string { return "" }

func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (f fakeTool) ReadOnly() bool { return f.readOnly }

func (f fakeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return f.name + " done", nil
}
