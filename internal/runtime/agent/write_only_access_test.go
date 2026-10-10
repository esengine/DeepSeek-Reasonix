package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

func TestPreToolAccessAllowsPathBoundWriteOnlyTargets(t *testing.T) {
	for _, name := range []string{"write_file", "move_file"} {
		t.Run(name, func(t *testing.T) {
			root := testenv.TempDir(t)
			destination := filepath.Join(root, "private", "new.txt")
			source := filepath.Join(root, "source.txt")
			const content = "synthetic content\n"
			if name == "move_file" {
				if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ws := builtin.Workspace{Dir: root, ForbidReadRoots: []string{filepath.Dir(destination)}}
			reg := tool.NewRegistry()
			reg.Add(ws.Tools(name)[0])
			claim, err := writeclaim.NormalizeWritePaths(root, []string{destination, source})
			if err != nil {
				t.Fatal(err)
			}
			bound, _ := BindWritePaths(reg, writeclaim.NewWriteGrant(claim), nil, writeclaim.NewSubagentScheduler(4, 2), root, false)
			a := New(nil, bound, sessionstore.NewSession(""), Options{CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
			args, err := json.Marshal(map[string]string{"path": destination, "content": content, "source_path": source, "destination_path": destination})
			if err != nil {
				t.Fatal(err)
			}
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "write-only", Name: name, Arguments: string(args)})
			if out.blocked || out.errMsg != "" {
				t.Fatalf("write-only target rejected: %+v", out)
			}
			if got, err := os.ReadFile(destination); err != nil || string(got) != content {
				t.Fatalf("destination=%q error=%v", got, err)
			}
		})
	}
}
