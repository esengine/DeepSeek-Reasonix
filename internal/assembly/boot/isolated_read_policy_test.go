package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/tools/builtin"
)

func TestIsolatedReadPolicyPreservesOriginalAndMappedDenials(t *testing.T) {
	for _, spelling := range []string{"absolute", "relative"} {
		t.Run(spelling, func(t *testing.T) {
			source, candidate := t.TempDir(), t.TempDir()
			for _, root := range []string{source, candidate} {
				if err := os.WriteFile(filepath.Join(root, "secret"), []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.Default()
			cfg.Sandbox.ForbidRead = []string{filepath.Join(source, "secret")}
			if spelling == "relative" {
				cfg.Sandbox.ForbidRead = []string{"secret"}
			}
			mapped, err := isolatedReadPolicy(cfg, source, candidate)
			if err != nil {
				t.Fatal(err)
			}
			workspace := builtin.Workspace{Dir: candidate, WriteRoots: []string{candidate}, ForbidReadRoots: mapped}
			reg := tool.NewRegistry()
			for _, reader := range workspace.Tools("read_file") {
				reg.Add(reader)
			}
			host := agent.NewHostExecutor(reg, agent.Options{CheckTargetAccess: workspace.TargetAccessCheck()}, event.Discard)
			defer host.Close()
			for _, root := range []string{source, candidate} {
				args, _ := json.Marshal(map[string]string{"path": filepath.Join(root, "secret")})
				result := host.Execute(context.Background(), provider.ToolCall{Name: "read_file", Arguments: string(args)})
				if result.Executed || !result.Blocked || result.RefusalCode == "" {
					t.Fatalf("isolated read bypassed %s rule for %s: %+v", spelling, root, result)
				}
			}
			if err := os.WriteFile(filepath.Join(candidate, "public"), []byte("visible"), 0600); err != nil {
				t.Fatal(err)
			}
			allowed := host.Execute(t.Context(), provider.ToolCall{Name: "read_file", Arguments: `{"path":"public"}`})
			if !allowed.Executed || allowed.Blocked || allowed.Error != "" {
				t.Fatalf("deny fixture cannot read public data: %+v", allowed)
			}
		})
	}
}

func TestUnattendedOptionsRebindInheritedReadPolicyWithoutDroppingSource(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	denied := filepath.Join(source, "secret")
	parent := Options{WorkspaceRoot: source, readPolicyRoot: source, inheritedReadDenies: []string{denied}}
	child, err := unattendedOptions(parent, event.Discard, "ask", target, "")
	if err != nil {
		t.Fatal(err)
	}
	var original, mapped bool
	for _, root := range child.inheritedReadDenies {
		original = original || pathComparisonKey(root) == pathComparisonKey(denied)
		mapped = mapped || pathComparisonKey(root) == pathComparisonKey(filepath.Join(target, "secret"))
	}
	if !original || !mapped {
		t.Fatalf("candidate lost source or copied-file deny: %v", child.inheritedReadDenies)
	}
	child.inheritedReadDenies[0] = "changed"
	if parent.inheritedReadDenies[0] != denied {
		t.Fatal("candidate policy aliases parent policy")
	}
}

func TestIsolatedReadPolicyPreservesAncestorDenial(t *testing.T) {
	for _, spelling := range []string{"absolute", "relative", "symlink"} {
		t.Run(spelling, func(t *testing.T) {
			ancestor, candidate := t.TempDir(), t.TempDir()
			source := filepath.Join(ancestor, "repo")
			if err := os.MkdirAll(source, 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.Sandbox.ForbidRead = []string{ancestor}
			switch spelling {
			case "relative":
				cfg.Sandbox.ForbidRead = []string{".."}
			case "symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(ancestor, alias); err != nil {
					t.Skipf("host cannot create a directory symlink: %v", err)
				}
				cfg.Sandbox.ForbidRead = []string{alias}
			}
			mapped, err := isolatedReadPolicy(cfg, source, candidate)
			if err != nil {
				t.Fatal(err)
			}
			workspace := builtin.Workspace{Dir: candidate, WriteRoots: []string{candidate}, ForbidReadRoots: mapped}
			reg := tool.NewRegistry()
			for _, reader := range workspace.Tools("read_file") {
				reg.Add(reader)
			}
			host := agent.NewHostExecutor(reg, agent.Options{CheckTargetAccess: workspace.TargetAccessCheck()}, event.Discard)
			defer host.Close()
			for _, root := range []string{source, candidate} {
				if err := os.WriteFile(filepath.Join(root, "secret"), []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				args, _ := json.Marshal(map[string]string{"path": filepath.Join(root, "secret")})
				result := host.Execute(t.Context(), provider.ToolCall{Name: "read_file", Arguments: string(args)})
				if result.Executed || !result.Blocked || result.RefusalCode == "" {
					t.Fatalf("ancestor rule %s bypassed at %s: %+v", spelling, root, result)
				}
			}
		})
	}
}
