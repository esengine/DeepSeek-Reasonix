package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/tools/builtin"
)

func TestWorkspaceClaimUsesTheWritersWhitespaceSemantics(t *testing.T) {
	root := canonicalTempDir(t)
	a := &Agent{}
	a.writeWorkspaceRoot = root
	for _, name := range []string{"write_file", "edit_file", "multi_edit", "notebook_edit", "delete_range", "delete_symbol", "move_file"} {
		t.Run(name, func(t *testing.T) {
			writer := (builtin.Workspace{Dir: root}).Tools(name)[0]
			args := json.RawMessage(`{"path":" leading.txt","source_path":" leading.txt","destination_path":" destination.txt","content":"fixture"}`)
			got := a.workspaceWritePaths(&toolCallPlan{runTool: writer, runArgs: args})
			want := []string{filepath.Join(root, " leading.txt")}
			if name == "move_file" {
				want = append(want, filepath.Join(root, " destination.txt"))
			}
			if len(got) != len(want) {
				t.Fatalf("paths: %v", got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("claimed %q; writer uses %q", got[i], want[i])
				}
			}
			if name == "write_file" {
				if _, err := writer.Execute(context.Background(), args); err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(got[0]); err != nil || string(data) != "fixture" {
					t.Fatalf("claim does not name written file: %q, %v", data, err)
				}
			}
		})
	}
}

func TestSubagentFencePreservesWriterWhitespace(t *testing.T) {
	root := canonicalTempDir(t)
	got, err := extractWritePathsFromArgs("move_file", root, json.RawMessage(`{"source_path":" source.txt","destination_path":" dest.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != filepath.Join(root, " source.txt") || got[1] != filepath.Join(root, " dest.txt") {
		t.Fatalf("fence differs from writer: %v", got)
	}
}

func TestWorkspaceClaimUsesBoundSubagentWriterPaths(t *testing.T) {
	root := canonicalTempDir(t)
	writer := (builtin.Workspace{Dir: root}).Tools("write_file")[0]
	bound := pathBoundWriter{inner: writer, workDir: root}
	a := &Agent{}
	a.writeWorkspaceRoot = filepath.Join(root, "another-selected-directory")
	paths := a.workspaceWritePaths(&toolCallPlan{runTool: bound, runArgs: json.RawMessage(`{"path":"child.txt"}`)})
	if len(paths) != 1 || paths[0] != filepath.Join(root, "child.txt") {
		t.Fatalf("bound writer lost its concrete scope: %v", paths)
	}
}

// Claims and grants are keyed by resolved paths, so fixtures that compare them
// against a spelled-out path start from a root with no symlinked ancestor.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	root, err := fileutil.ResolveExistingPath(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
