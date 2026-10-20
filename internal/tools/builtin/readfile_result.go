package builtin

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

type readFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (readFile) Provenance(args json.RawMessage) tool.Provenance {
	var p readFileArgs
	if json.Unmarshal(args, &p) == nil && tool.IsResultReference(p.Path) {
		return tool.Provenance{Kind: tool.ProvenanceMCP, Source: "tool-result"}
	}
	return tool.Provenance{Kind: tool.ProvenanceWorkspace}
}

func (r readFile) readResult(ctx context.Context, p readFileArgs) (string, error) {
	f, err := tool.OpenResult(ctx, p.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Limit <= 0 {
		p.Limit = readFileDefaultLimit
	}
	return r.scan(f, p.Offset, p.Limit)
}
