package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

func TestBindChildWriteRootsSnapshotsParentGrants(t *testing.T) {
	work := t.TempDir()
	extra := t.TempDir()
	parent := sandbox.NewWritableRootSet([]string{work})
	parent.GrantSession([]string{extra})
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: work, WriteRoots: []string{work}, WriteRootSet: parent}).Tools("write_file") {
		if tl.Name() == "write_file" {
			reg.Add(tl)
		}
	}
	_, child := BindChildWriteRoots(reg, parent, WritePathSet{})
	later := t.TempDir()
	parent.GrantSession([]string{later})
	if child.Covers(later) {
		t.Fatal("child must not inherit later parent grants")
	}
	if !child.Covers(extra) {
		t.Fatal("child should inherit the snapshot that existed at spawn")
	}

	pkg := filepath.Join(work, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	claims, err := NormalizeWritePaths(work, []string{"pkg"})
	if err != nil {
		t.Fatal(err)
	}
	_, restricted := BindChildWriteRoots(reg, parent, claims)
	if restricted.Covers(extra) {
		t.Fatal("explicit write_paths must drop unrelated session grants")
	}
	if !restricted.Covers(pkg) {
		t.Fatal("explicit write_paths should keep the intersection")
	}

	whole, err := WholeWorkspaceWriteClaim(work)
	if err != nil {
		t.Fatal(err)
	}
	_, inherited := BindChildWriteRoots(reg, parent, whole)
	if !inherited.Covers(extra) {
		t.Fatal("omitted write_paths still inherit the existing session snapshot")
	}
}

func TestApplyWriteAccessSubagentUsesStructuredHint(t *testing.T) {
	work := t.TempDir()
	outside := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{work})
	a := &Agent{svc: agentServices{
		writeRoots:            set,
		writeAccessExpandable: false,
		workspaceRoot:         work,
		homeDir:               work,
		stateRoot:             t.TempDir(),
	}}
	var write tool.Tool
	for _, tl := range (builtin.Workspace{Dir: work, WriteRoots: []string{work}, WriteRootSet: set}).Tools("write_file") {
		if tl.Name() == "write_file" {
			write = tl
		}
	}
	if write == nil {
		t.Fatal("write_file missing")
	}
	args, err := json.Marshal(map[string]string{"path": filepath.Join(outside, "x.go"), "content": "x"})
	if err != nil {
		t.Fatal(err)
	}
	out, early := a.applyWriteAccess(context.Background(), &toolCallPlan{
		execTool: write,
		permName: "write_file",
		permArgs: args,
	})
	if !early || !out.blocked {
		t.Fatalf("expected blocked write access, got %+v early=%v", out, early)
	}
	if !strings.Contains(out.output, "parent agent") {
		t.Fatalf("sub-agent hint missing: %s", out.output)
	}
}
