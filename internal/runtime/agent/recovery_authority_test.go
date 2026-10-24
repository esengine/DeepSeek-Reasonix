package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

type recoveryScopeTool struct{ root, approved, actual string }

func (recoveryScopeTool) Name() string              { return "recover_integration" }
func (recoveryScopeTool) Description() string       { return "Recover the previewed path" }
func (recoveryScopeTool) Schema() json.RawMessage   { return json.RawMessage(`{"type":"object"}`) }
func (recoveryScopeTool) ReadOnly() bool            { return false }
func (recoveryScopeTool) RecoversIntegration() bool { return true }
func (t recoveryScopeTool) DeclaredWritePaths(context.Context, json.RawMessage) ([]string, error) {
	return []string{t.approved}, nil
}
func (t recoveryScopeTool) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationNamedPaths, Paths: []string{t.approved}, SharedResources: []string{t.root}}, nil
}
func (t recoveryScopeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := worktree.AuthorizeRecoveryPaths(ctx, []string{t.actual}); err != nil {
		return "", err
	}
	return "restored", os.WriteFile(t.actual, []byte("recovered"), 0600)
}

func TestRecoverySharedResourcesCannotExpandApprovedPathSet(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "approved", true: "changed-after-approval"}[changed], func(t *testing.T) {
			root := t.TempDir()
			approved := filepath.Join(root, "approved")
			actual := approved
			if changed {
				actual = filepath.Join(root, "unapproved")
			}
			c, err := mutation.New(root, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			reg := tool.NewRegistry()
			reg.Add(recoveryScopeTool{root: root, approved: approved, actual: actual})
			ws := builtin.Workspace{Dir: root, ReadRoots: []string{root}}
			a := New(nil, reg, sessionstore.NewSession(""), Options{WriteWorkspaceRoot: root, MutationCoordinator: c, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
			out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "recover", Name: "recover_integration", Arguments: `{}`})
			_, statErr := os.Stat(actual)
			if changed {
				if out.errMsg == "" || !os.IsNotExist(statErr) {
					t.Fatalf("coordination root expanded recovery authority: %+v %v", out, statErr)
				}
			} else if out.errMsg != "" || statErr != nil {
				t.Fatalf("approved recovery was denied: %+v %v", out, statErr)
			}
		})
	}
}

func TestRecoveryCannotUseManagedConfigPreviewException(t *testing.T) {
	for _, granted := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview-only", true: "explicit-write-root"}[granted], func(t *testing.T) {
			repo, state := t.TempDir(), t.TempDir()
			workspace := filepath.Join(repo, "app")
			if err := os.Mkdir(workspace, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(repo, "config.toml")
			if err := os.WriteFile(path, []byte("original config"), 0600); err != nil {
				t.Fatal(err)
			}
			ws := builtin.Workspace{Dir: workspace, ManagedConfig: builtin.NewManagedConfigPaths([]string{path})}
			if granted {
				ws.WriteRoots = []string{workspace, repo}
			}
			c, err := mutation.New(workspace, state, nil)
			if err != nil {
				t.Fatal(err)
			}
			reg := tool.NewRegistry()
			reg.Add(recoveryScopeTool{root: repo, approved: path, actual: path})
			a := New(nil, reg, sessionstore.NewSession(""), Options{WriteWorkspaceRoot: workspace, MutationCoordinator: c, CheckTargetAccess: ws.TargetAccessCheck()}, event.Discard)
			out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "recover", Name: "recover_integration", Arguments: `{}`})
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if granted {
				if out.errMsg != "" || string(data) != "recovered" {
					t.Fatalf("explicit write scope was denied: %+v %q", out, data)
				}
			} else if out.errMsg == "" || string(data) != "original config" {
				t.Fatalf("preview exception authorized configuration mutation: %+v %q", out, data)
			}
		})
	}
}
