package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/workspacelease"
)

type partialLeaseWriter struct {
	workspaceLeaseTestTool
	path string
}

func (w *partialLeaseWriter) WritesNamedPaths() bool { return true }
func (w *partialLeaseWriter) WritePaths(json.RawMessage) ([]string, error) {
	return []string{w.path}, nil
}
func (w *partialLeaseWriter) Execute(context.Context, json.RawMessage) (string, error) {
	w.calls.Add(1)
	if err := os.WriteFile(w.path, []byte("partial\n"), 0o600); err != nil {
		return "", err
	}
	return "", errors.New("failed after changing the file")
}

func TestProvisionalLeaseRetainsHookAndToolPartialWrites(t *testing.T) {
	for _, hookDenies := range []bool{false, true} {
		t.Run(map[bool]string{false: "tool_failure", true: "hook_denial"}[hookDenies], func(t *testing.T) {
			root, locks := testenv.TempDir(t), testenv.TempDir(t)
			owner, err := workspacelease.New(root, locks, nil)
			if err != nil {
				t.Fatal(err)
			}
			probe, err := workspacelease.New(root, locks, nil)
			if err != nil {
				t.Fatal(err)
			}
			owner.BeginRun()
			defer owner.EndRun()
			probe.BeginRun()
			defer probe.EndRun()
			writer := &partialLeaseWriter{workspaceLeaseTestTool: workspaceLeaseTestTool{name: "partial_writer"}, path: filepath.Join(root, "written")}
			a := deliveryLeaseTestAgent(t, owner, writer)
			want := "partial\n"
			if hookDenies {
				a.svc.hooks = &denyingWritingHook{stubHooks: &stubHooks{}, path: writer.path}
				want = "hook changed\n"
			}
			out := a.executeOne(t.Context(), &a.turn, providerToolCall("partial", writer.Name()))
			if hookDenies && (!out.blocked || writer.calls.Load() != 0) || !hookDenies && (out.errMsg == "" || writer.calls.Load() != 1) {
				t.Fatalf("incorrect mutation-entry classification: %+v", out)
			}
			if content, err := os.ReadFile(writer.path); err != nil || string(content) != want {
				t.Fatalf("partial write = %q, %v", content, err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			if err := probe.AcquirePaths(ctx, []string{writer.path}); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("partial write lost its retained protection: %v", err)
			}
			owner.EndRun()
			if err := probe.AcquirePaths(t.Context(), []string{writer.path}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
