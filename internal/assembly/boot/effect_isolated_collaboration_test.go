package boot

import (
	"context"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"strings"
	"testing"
)

func TestEffectIsolatedDelegationKeepsReportsContextAndTranscriptOwnership(t *testing.T) {
	home, dir := statedBootHome(t), robustTempDir(t)
	p := &collaborationProbe{isolated: true}
	kind := uniqueKind("isolated-collaboration")
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `default_model = "probe"
[agent]
worktree_isolation = true
[codegraph]
enabled = false
[[providers]]
name = "probe"
kind = "`+kind+`"
model = "probe"
`)
	writeFile(t, dir, "source.txt", "PRIVATE-CHILD-READ")
	mirrorToUserConfigAt(t, home, dir)
	gitInDir(t, dir, "init", "-q")
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-qm", "fixture")
	c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.EnsureSessionPath()
	c.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := c.Run(context.Background(), "Inspect the parser using a child"); err != nil {
		t.Fatal(err)
	}
	runs, err := c.SubagentRuns()
	if err != nil || len(runs) != 1 {
		t.Fatalf("isolated child missing from parent directory: %v %v", runs, err)
	}
	page, err := c.SubagentTranscript(runs[0].Ref, -1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) == 0 {
		t.Fatal("missing isolated transcript")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) != 5 {
		t.Fatalf("requests=%d", len(p.requests))
	}
	if !requestContains(p.requests[1], "The parser must preserve empty fields") || !requestContains(p.requests[4], "I found the parser") {
		t.Fatal("lost selected context or parent report")
	}
	for _, schema := range p.requests[1].Tools {
		if strings.Contains(schema.Name, "write_file") || schema.Name == "team" {
			t.Fatal("isolated child widened its tools")
		}
	}
}
