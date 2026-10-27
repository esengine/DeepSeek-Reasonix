package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/tools/builtin"
)

func TestRoleFileViewsDoNotRefreshExecutor(t *testing.T) {
	for _, role := range []string{"planner", "guardian"} {
		for _, shared := range []bool{false, true} {
			t.Run(role+map[bool]string{false: "/clone", true: "/runtime"}[shared], func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "a.txt")
				if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
					t.Fatal(err)
				}
				root := fileViewRegistry(t)
				parentLedger, roleLedger := capability.NewLedger(), capability.NewLedger()
				parentAudit, roleAudit := &capability.Audit{}, &capability.Audit{}
				runtime := usecap.NewMCPCapabilityRuntime(t.Context(), nil, nil, root, nil)
				if shared {
					root.Add(runtime.NewFrontend(parentLedger, parentAudit))
				} else {
					root.Add(usecap.NewUseCapabilityTool(t.Context(), nil, nil, root, parentLedger, parentAudit, nil))
				}
				if err := callFileViewTool(t, root, "read_file", map[string]string{"path": path}); err != nil {
					t.Fatal(err)
				}
				var reg *tool.Registry
				if role == "guardian" {
					reg = GuardianToolRegistry(root)
				} else if shared {
					reg = PlannerToolRegistryWithFrontend(root, runtime.NewFrontend(roleLedger, roleAudit))
				} else {
					reg = PlannerToolRegistry(root)
				}
				if _, ok := reg.Get("write_file"); ok {
					t.Fatal("role gained a direct writer")
				}
				if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
					t.Fatal(err)
				}
				args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:read_file", "arguments": map[string]string{"path": path}})
				proxy, ok := reg.Get("use_capability")
				if !ok {
					t.Fatal("missing role proxy")
				}
				if _, err := proxy.Execute(t.Context(), args); err != nil {
					t.Fatal(err)
				}
				if len(parentLedger.Snapshot()) != 0 || parentAudit.Snapshot().MCPCall != 0 {
					t.Fatal("role call changed executor capability accounting")
				}
				if role == "planner" && shared {
					entry, ok := roleLedger.Get("tool:read_file")
					if !ok || entry.Outcome != capability.OutcomeSucceeded || roleAudit.Snapshot().MCPCall != 1 {
						t.Fatal("role binding discarded planner ledger or audit")
					}
				}
				err := callFileViewTool(t, root, "write_file", map[string]string{"path": path, "content": "stale"})
				if !errors.Is(err, builtin.ErrFileChangedSinceSeen) {
					t.Fatalf("role refreshed executor view: %v", err)
				}
				// A new root read must still refresh the root's own writer.
				rootProxy, _ := root.Get("use_capability")
				if _, err := rootProxy.Execute(t.Context(), args); err != nil {
					t.Fatal(err)
				}
				if err := callFileViewTool(t, root, "write_file", map[string]string{"path": path, "content": "fresh"}); err != nil {
					t.Fatalf("role binding changed parent proxy/direct state: %v", err)
				}
			})
		}
	}
}

func TestGenericReadOnlyFilterKeepsSameActorViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	root := fileViewRegistry(t)
	filtered := FilterReadOnlyRegistry(root)
	if err := callFileViewTool(t, filtered, "read_file", map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := callFileViewTool(t, filtered, "read_file", map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := callFileViewTool(t, root, "write_file", map[string]string{"path": path, "content": "fresh"}); err != nil {
		t.Fatalf("filter unexpectedly created another actor: %v", err)
	}
}
