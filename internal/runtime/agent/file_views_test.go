package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/tools/builtin"
)

func fileViewRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	views := builtin.NewFileViews()
	for _, tl := range tool.Builtins() {
		if tl.Name() == "read_file" || tl.Name() == "write_file" {
			reg.Add(builtin.BindFileViews(tl, views))
		}
	}
	return reg
}

func callFileViewTool(t *testing.T, reg *tool.Registry, name string, args map[string]string) error {
	t.Helper()
	tl, ok := reg.Get(name)
	if !ok {
		t.Fatalf("missing %s", name)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tl.Execute(t.Context(), raw)
	return err
}

func TestChildReadsDoNotRefreshParentFileViews(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "writer", true: "reader"}[readOnly], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.txt")
			if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
				t.Fatal(err)
			}
			parent := fileViewRegistry(t)
			if err := callFileViewTool(t, parent, "read_file", map[string]string{"path": path}); err != nil {
				t.Fatal(err)
			}
			child := SubagentToolRegistry(parent, nil)
			if readOnly {
				child = ReadOnlySubagentToolRegistry(parent, nil)
			}
			if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := callFileViewTool(t, child, "read_file", map[string]string{"path": path}); err != nil {
				t.Fatal(err)
			}
			err := callFileViewTool(t, parent, "write_file", map[string]string{"path": path, "content": "stale parent"})
			if !errors.Is(err, builtin.ErrFileChangedSinceSeen) {
				t.Fatalf("child read refreshed parent: %v", err)
			}
		})
	}
}

func TestChildCapabilityReadsKeepActorFileViews(t *testing.T) {
	for _, depth := range []int{1, 2, 3} {
		for _, shared := range []bool{false, true} {
			t.Run(strconv.Itoa(depth)+"/shared="+strconv.FormatBool(shared), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "a.txt")
				if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
					t.Fatal(err)
				}
				root := fileViewRegistry(t)
				var runtime *usecap.MCPCapabilityRuntime
				if shared {
					runtime = usecap.NewMCPCapabilityRuntime(t.Context(), nil, nil, root, nil)
				} else {
					root.Add(usecap.NewUseCapabilityTool(t.Context(), nil, nil, root, nil, nil, nil))
				}
				parent := root
				for d := 1; d < depth; d++ {
					parent = SubagentToolRegistryForDepthWithRuntime(parent, nil, d, 3, runtime)
				}
				if err := callFileViewTool(t, parent, "read_file", map[string]string{"path": path}); err != nil {
					t.Fatal(err)
				}
				child := SubagentToolRegistryForDepthWithRuntime(parent, nil, depth, 3, runtime)
				sibling := SubagentToolRegistryForDepthWithRuntime(parent, nil, depth, 3, runtime)
				for _, reg := range []*tool.Registry{child, sibling} {
					if err := callFileViewTool(t, reg, "read_file", map[string]string{"path": path}); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
					t.Fatal(err)
				}
				proxy, ok := child.Get("use_capability")
				if !ok {
					t.Fatal("missing capability frontend")
				}
				args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:read_file", "arguments": map[string]string{"path": path}})
				resolved, err := proxy.(tool.CallResolver).ResolveCall(t.Context(), args)
				if err != nil || resolved.Target == nil {
					t.Fatalf("resolve: %+v %v", resolved, err)
				}
				if _, err := resolved.Target.Execute(t.Context(), resolved.Args); err != nil {
					t.Fatal(err)
				}
				for name, reg := range map[string]*tool.Registry{"parent": parent, "sibling": sibling} {
					err := callFileViewTool(t, reg, "write_file", map[string]string{"path": path, "content": "stale"})
					if !errors.Is(err, builtin.ErrFileChangedSinceSeen) {
						t.Fatalf("proxy polluted %s: %v", name, err)
					}
				}
				if err := callFileViewTool(t, child, "write_file", map[string]string{"path": path, "content": "child update"}); err != nil {
					t.Fatalf("proxy read did not refresh its own direct write: %v", err)
				}
			})
		}
	}
}
