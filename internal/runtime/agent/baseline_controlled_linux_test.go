//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/builtin"
)

func capturedGoRuntime(t *testing.T) string {
	t.Helper()
	image := testenv.VerificationRuntime(t)
	err := filepath.WalkDir(runtime.GOROOT(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(runtime.GOROOT(), path)
		if err != nil {
			return err
		}
		target := filepath.Join(image, "usr/local", rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func TestControlledBaselineRunsCapturedGoWithoutRestoringToolsOrWritingSource(t *testing.T) {
	canVerify := sandbox.AvailableFor(sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity})
	t.Logf("controlled verification available: %t", canVerify)
	image := t.TempDir()
	if canVerify {
		image = capturedGoRuntime(t)
	}
	for _, mode := range []string{"pass", "fail", "disabled", "denied", "postwrite", "posterror", "stale-after"} {
		t.Run(mode, func(t *testing.T) {
			impl := "package spike\nfunc Evict(n int) int { return n-1 }\n"
			if mode == "fail" {
				impl = "package spike\nfunc Evict(n int) int { return n }\n"
			}
			fixture := baselineWorkspace(t, impl, assertsNothing)
			root := fixture.writeWorkspaceRoot
			reg := tool.NewRegistry()
			if mode != "disabled" {
				for _, entry := range (builtin.Workspace{Dir: root, WriteRoots: []string{root}, Bash: sandbox.Spec{Mode: "enforce", MinimalWrites: true, WriteRoots: []string{root}}}).Tools("bash") {
					reg.Add(entry)
				}
			}
			opts := Options{ProcessSandbox: sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity},
				CheckEnvironment:   &checkenv.Spec{RuntimeRoot: image, SourceRoot: root, Directory: root, StorageRoot: t.TempDir()},
				WriteWorkspaceRoot: root, ArchiveDir: fixture.archiveDir}
			if mode == "denied" {
				opts.Gate = denyCapturedBaseline{}
			}
			if mode == "postwrite" {
				opts.Hooks = &capturedPostWrite{root: root, t: t}
			}
			if mode == "posterror" {
				client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
					return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "fixture rejects final verification"}, nil
				}}
				opts.Extensions = newExtDispatcher(client, true, nil, extension.PointToolAfter)
			}
			a := New(nil, reg, sessionstore.NewSession(""), opts, event.Discard)
			a.task.baselineCriteria = fixture.task.baselineCriteria
			a.evaluateBaselineCriteriaOnce(t.Context())
			if mode == "stale-after" {
				if err := os.WriteFile(filepath.Join(root, "cache.go"), []byte("package spike\nfunc Evict(n int) int { return n }\n"), 0600); err != nil {
					t.Fatal(err)
				}
				facts := a.baselineFacts()
				if len(facts) != 1 || len(evidence.BaselineTestObligations(facts, a.mutationEpoch())) != 1 ||
					(canVerify && facts[0].Evaluation != nil) ||
					(!canVerify && facts[0].Evaluation != nil && facts[0].Evaluation.Result != evidence.CriterionUnavailable) {
					t.Fatalf("stale receipt settled: %+v", facts)
				}
				return
			}
			facts := a.baselineFacts()
			want := evidence.CriterionUnavailable
			if canVerify && mode == "pass" {
				want = evidence.CriterionPass
			}
			if canVerify && mode == "fail" {
				want = evidence.CriterionFail
			}
			if len(facts) != 1 || facts[0].Evaluation == nil || facts[0].Evaluation.Result != want {
				t.Fatalf("%s: %+v", mode, facts)
			}
			current, err := os.ReadFile(filepath.Join(root, "cache_test.go"))
			if err != nil || string(current) != assertsNothing {
				t.Fatalf("source changed: %q %v", current, err)
			}
			if facts[0].Evaluation.Backend != "controlled_go_snapshot" {
				t.Fatal("wrong execution backend")
			}
		})
	}
}

type capturedPostWrite struct {
	stubHooks
	root string
	t    *testing.T
}

func (h *capturedPostWrite) PostToolUse(context.Context, string, json.RawMessage, string) {
	if err := os.WriteFile(filepath.Join(h.root, "cache.go"), []byte("package spike\nfunc Evict(n int) int { return n }\n"), 0600); err != nil {
		h.t.Fatal(err)
	}
}

type denyCapturedBaseline struct{}

func (denyCapturedBaseline) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	return false, "fixture denies captured execution", nil
}
