//go:build linux

package boot

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/isolation"
)

func TestEffectNestedControlledIntegrationPreservesLiveAncestorAuthority(t *testing.T) {
	for _, mode := range []string{"allowed", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			p := &nestedExecutionProvider{results: map[string]string{}}
			var checks atomic.Int32
			ctrl, root := nestedExecutionFixture(t, p, testenv.VerificationRuntime(t), "[ -f child.txt ]", event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Issuer == event.IssuedByHost && e.Tool.Name == "bash" && e.Tool.Err == "" {
					checks.Add(1)
				}
			}))
			if mode == "revoked" {
				p.onLeaf = func() { ctrl.SetPlanMode(true) }
			}
			_ = ctrl.Run(t.Context(), "run the nested isolation fixture")
			raw, err := os.ReadFile(filepath.Join(root, "child.txt"))
			catalog, catalogErr := ctrl.IsolationResults(t.Context())
			if catalogErr != nil {
				t.Fatal(catalogErr)
			}
			if mode == "allowed" {
				if err != nil || string(raw) != "nested candidate\n" || checks.Load() != 2 {
					p.mu.Lock()
					defer p.mu.Unlock()
					t.Fatalf("nested publication failed: %q %v checks=%d catalog=%+v results=%v", raw, err, checks.Load(), catalog, p.results)
				}
				for _, row := range catalog.Results {
					if row.State != isolation.Integrated || !row.CleanupComplete {
						t.Fatalf("successful nested publication did not settle every dependency: %+v", row)
					}
				}
				p.mu.Lock()
				defer p.mu.Unlock()
				if !strings.Contains(p.results["parent"], "verification belongs to that exact integrated snapshot") {
					t.Fatalf("nested result lost exact snapshot attribution: %v", p.results)
				}
				if strings.Contains(p.results["parent"], "OUTSIDE DECLARED write_paths") {
					t.Fatalf("candidate grant audit used the wrong workspace: %v", p.results)
				}
			} else {
				if !os.IsNotExist(err) {
					t.Fatalf("revoked ancestor allowed nested publication: %q %v", raw, err)
				}
				found := false
				for _, row := range catalog.Results {
					if row.ParentID != "" && row.Kind == "result" {
						found = row.State != isolation.Integrated && !row.CleanupComplete
					}
				}
				if !found {
					t.Fatalf("refused nested publication lost the descendant result: %+v", catalog)
				}
			}
		})
	}
}
