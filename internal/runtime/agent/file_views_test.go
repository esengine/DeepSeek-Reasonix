package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/tools/builtin"
)

func TestChildReadsDoNotRefreshParentFileViews(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "writer", true: "reader"}[readOnly], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.txt")
			if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
				t.Fatal(err)
			}
			parent := tool.NewRegistry()
			views := builtin.NewFileViews()
			for _, tl := range tool.Builtins() {
				if tl.Name() == "read_file" || tl.Name() == "write_file" {
					parent.Add(builtin.BindFileViews(tl, views))
				}
			}
			call := func(reg *tool.Registry, name string, args map[string]string) error {
				tl, ok := reg.Get(name)
				if !ok {
					t.Fatalf("missing %s", name)
				}
				raw, _ := json.Marshal(args)
				_, err := tl.Execute(context.Background(), raw)
				return err
			}
			if err := call(parent, "read_file", map[string]string{"path": path}); err != nil {
				t.Fatal(err)
			}
			child := SubagentToolRegistry(parent, nil)
			if readOnly {
				child = ReadOnlySubagentToolRegistry(parent, nil)
			}
			if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := call(child, "read_file", map[string]string{"path": path}); err != nil {
				t.Fatal(err)
			}
			err := call(parent, "write_file", map[string]string{"path": path, "content": "stale parent"})
			if !errors.Is(err, builtin.ErrFileChangedSinceSeen) {
				t.Fatalf("child read refreshed parent: %v", err)
			}
		})
	}
}

func TestChildCapabilityReadsDoNotRefreshParentFileViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	parent := tool.NewRegistry()
	views := builtin.NewStrictFileViews()
	for _, tl := range tool.Builtins() {
		if tl.Name() == "read_file" || tl.Name() == "write_file" {
			parent.Add(builtin.BindFileViews(tl, views))
		}
	}
	runtime := usecap.NewMCPCapabilityRuntime(t.Context(), nil, nil, parent, nil)
	reader, _ := parent.Get("read_file")
	raw, _ := json.Marshal(map[string]string{"path": path})
	if _, err := reader.Execute(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	child := SubagentToolRegistryForDepthWithRuntime(parent, nil, 1, 1, runtime)
	sibling := SubagentToolRegistryForDepthWithRuntime(parent, nil, 1, 1, runtime)
	siblingReader, _ := sibling.Get("read_file")
	if _, err := siblingReader.Execute(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	proxy, _ := child.Get("use_capability")
	args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:read_file", "arguments": map[string]string{"path": path}})
	resolved, err := proxy.(tool.CallResolver).ResolveCall(t.Context(), args)
	if err != nil || resolved.Target == nil {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	if _, err := resolved.Target.Execute(t.Context(), resolved.Args); err != nil {
		t.Fatal(err)
	}
	writer, _ := parent.Get("write_file")
	writeArgs, _ := json.Marshal(map[string]string{"path": path, "content": "stale parent"})
	if _, err := writer.Execute(t.Context(), writeArgs); !errors.Is(err, builtin.ErrFileChangedSinceSeen) {
		t.Fatalf("proxy polluted parent: %v", err)
	}
	siblingWriter, _ := sibling.Get("write_file")
	if _, err := siblingWriter.Execute(t.Context(), writeArgs); !errors.Is(err, builtin.ErrFileChangedSinceSeen) {
		t.Fatalf("proxy polluted sibling: %v", err)
	}
	childWriter, _ := child.Get("write_file")
	if _, err := childWriter.Execute(t.Context(), writeArgs); err != nil {
		t.Fatalf("proxy read did not share child view: %v", err)
	}
}
