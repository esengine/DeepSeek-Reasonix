package tool

import (
	"context"
	"encoding/json"
)

// WritePathDeclarer is a writer that can name, before it runs, every path it
// will write — a set too wide for one Previewer change. The host reserves those
// paths, captures each one's preimage and records them on the receipt, so the
// call is observed and rewindable like a single-file write. An error means the
// call cannot say, and it is treated as any undeclared writer.
type WritePathDeclarer interface {
	DeclaredWritePaths(ctx context.Context, args json.RawMessage) ([]string, error)
}

// WriteAuthorizer resolves any grant expansion before a mutation lease is held.
// Implementations must not mutate files or reserve execution resources.
type WriteAuthorizer interface {
	AuthorizeWrite(context.Context, json.RawMessage) error
}

// IntegrationRecovery marks host tools that settle a durable integration journal.
// Their declared resources must include every path recovery may restore.
type IntegrationRecovery interface {
	RecoversIntegration() bool
}
