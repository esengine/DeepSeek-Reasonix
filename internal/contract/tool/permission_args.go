package tool

import (
	"context"
	"encoding/json"
)

// PermissionProjector is a tool whose authorization turns on host state its
// arguments do not carry, such as the site a browser call acts on.
// PermissionArgs returns what the permission gate reads in place of args; the
// call itself, its hooks and its evidence keep args as sent.
type PermissionProjector interface {
	PermissionArgs(ctx context.Context, args json.RawMessage) json.RawMessage
}

// PermissionArgs is what the gate reads for a call to t.
func PermissionArgs(ctx context.Context, t Tool, args json.RawMessage) json.RawMessage {
	if p, ok := t.(PermissionProjector); ok {
		return p.PermissionArgs(ctx, args)
	}
	return args
}
