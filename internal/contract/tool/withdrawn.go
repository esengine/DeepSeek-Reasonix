package tool

import (
	"context"
	"encoding/json"
	"errors"
)

// withdrawnTool holds a provider schema slot open for a tool that is gone: the
// visible array is fixed for the session, so vacating an entry rewrites the
// cached prefix and costs every token behind it. The entry stays byte for byte
// and its call fails with the reason. Embedding the Tool interface strips the
// rest — an MCP binding, a preview — so catalogs stop offering it as live.
type withdrawnTool struct {
	Tool
	reason string
}

// Withdraw returns t as a schema-only placeholder that fails with reason.
// Withdrawing an already-withdrawn tool restates the reason rather than
// wrapping it twice.
func Withdraw(t Tool, reason string) Tool {
	if t == nil {
		return nil
	}
	if w, ok := t.(withdrawnTool); ok {
		return withdrawnTool{Tool: w.Tool, reason: reason}
	}
	return withdrawnTool{Tool: t, reason: reason}
}

// IsWithdrawn reports whether t is only holding a schema slot. Callers that
// route by what a tool can still do read this rather than calling it to find out.
func IsWithdrawn(t Tool) bool {
	_, ok := t.(withdrawnTool)
	return ok
}

func (w withdrawnTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", errors.New(w.reason)
}

// ReadOnly is true because the call only states its reason: approving a
// withdrawn tool would be approving an error message.
func (w withdrawnTool) ReadOnly() bool { return true }
