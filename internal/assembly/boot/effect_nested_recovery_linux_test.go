//go:build linux

package boot

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/isolation"
)

func TestEffectNestedPublicationRecoveryUsesRootControllerAndPhysicalJournal(t *testing.T) {
	p := &nestedExecutionProvider{results: map[string]string{}, extraFile: true}
	previous := fileutil.CrashPoint
	t.Cleanup(func() { fileutil.CrashPoint = previous })
	var once sync.Once
	var crashed atomic.Bool
	var activeSource atomic.Value
	activeSource.Store("")
	fileutil.CrashPoint = func(op, _ string) {
		source := activeSource.Load().(string)
		if source == "" || op != "atomic-write" {
			return
		}
		first, firstErr := os.ReadFile(filepath.Join(source, "child.txt"))
		_, secondErr := os.Stat(filepath.Join(source, "other.txt"))
		if firstErr == nil && string(first) == "nested candidate\n" && os.IsNotExist(secondErr) && crashed.CompareAndSwap(false, true) {
			panic("interrupted nested publication after first file")
		}
	}
	ctrl, root := nestedExecutionFixture(t, p, testenv.VerificationRuntime(t), "[ -f child.txt ] && [ -f other.txt ]", event.Discard)
	var source string
	p.onLeaf = func() {
		once.Do(func() {
			catalog, err := ctrl.IsolationResults(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range catalog.Results {
				if row.Kind == "result" && row.ParentID != "" {
					source = row.TargetWorkspace
				}
			}
			if source == "" {
				t.Fatal("actual delegated leaf has no registered source")
			}
			activeSource.Store(source)
		})
	}
	_ = ctrl.Run(t.Context(), "run the nested recovery fixture")
	activeSource.Store("")
	if !crashed.Load() {
		p.mu.Lock()
		defer p.mu.Unlock()
		t.Fatalf("real nested automatic integration did not reach the journal fault: %v", p.results)
	}
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var target *isolation.RecoveryTarget
	for _, row := range catalog.Results {
		if row.Recovery != nil {
			target = row.Recovery
			break
		}
	}
	if target == nil || target.Workspace != source || target.Runtime == "" {
		t.Fatalf("controller lost descendant recovery binding: %+v", catalog)
	}
	if err := ctrl.RecoverIsolatedIntegration(t.Context(), target.Request()); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{root, source} {
		for _, name := range []string{"child.txt", "other.txt"} {
			if _, err := os.Stat(filepath.Join(workspace, name)); !os.IsNotExist(err) {
				t.Fatalf("recovery did not restore source preimage: %s %s %v", workspace, name, err)
			}
		}
	}
	catalog, err = ctrl.IsolationResults(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Results {
		if row.ParentID != "" && (row.State != isolation.Conflict || row.CleanupComplete || row.Recovery != nil || row.Verification != "unknown") {
			t.Fatalf("recovered nested result lost recoverability or invented evidence: %+v", row)
		}
	}
}
