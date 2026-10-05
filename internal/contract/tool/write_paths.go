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
