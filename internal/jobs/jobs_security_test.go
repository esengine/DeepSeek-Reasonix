package jobs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/store"
)

// TestValidatePathSegment exhaustively covers the segment validator that guards
// StartForSession against #6932 (path traversal in artifact paths).
func TestValidatePathSegment(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		input   string
		wantErr bool
	}{
		// parentSession: empty is the documented unscoped default.
		{"empty parentSession is allowed", "parentSession", "", false},
		// kind: empty is rejected because id is built from kind.
		{"empty kind is rejected", "kind", "", true},
		// Typical safe values.
		{"simple lowercase kind", "kind", "task", false},
		{"hyphenated kind", "kind", "bash-bg", false},
		{"underscored kind", "kind", "bash_bg", false},
		{"digits-only kind", "kind", "123", false},
		{"unicode kind (no separators)", "kind", "任务", false},
		{"parentSession with timestamp", "parentSession", "20260724-142525abc", false},
		// Path separators: must reject on any field.
		{"forward slash", "kind", "task/evil", true},
		{"back slash", "kind", "task\\evil", true},
		{"only slash", "parentSession", "/", true},
		{"only backslash", "parentSession", "\\", true},
		// Traversal: must reject both `.` and `..` even without separators,
		// because filepath.Clean treats them as parent references.
		{"single dot", "kind", ".", true},
		{"double dot", "kind", "..", true},
		{"traversal prefix", "parentSession", "..", true},
		{"parentSession with traversal", "parentSession", "../../etc", true},
		{"kind with embedded traversal", "kind", "task/../../etc", true},
		// Control characters and NUL.
		{"NUL byte", "kind", "task\x00evil", true},
		{"newline", "kind", "task\nevil", true},
		{"tab", "kind", "task\tevil", true},
		{"DEL char", "kind", "task\x7fevil", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePathSegment(tc.input, tc.field)
			if tc.wantErr && err == nil {
				t.Fatalf("validatePathSegment(%q, %q) = nil, want error", tc.input, tc.field)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validatePathSegment(%q, %q) = %v, want nil", tc.input, tc.field, err)
			}
		})
	}
}

// TestStartForSession_RejectsPathTraversalParentSession ensures a malicious
// parentSession like "../../etc" does not create files outside the manager's
// temp root. See #6932.
func TestStartForSession_RejectsPathTraversalParentSession(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	// Snapshot the temp root before; afterwards the directory listing must be
	// unchanged. This proves no traversal payload created any subdirectory.
	beforeEntries, err := os.ReadDir(m.tempRoot)
	if err != nil {
		t.Fatalf("read temp root before: %v", err)
	}

	// "../../etc" with a leading traversal walks two parents up from
	// m.tempRoot and then descends into "etc". The exact resolution does not
	// matter; what matters is that the validator rejects it before any
	// mkdir or open happens, anywhere.
	ran := false
	j := m.StartForSession("../../etc", "task", "escape attempt", func(_ context.Context, _ io.Writer) (string, error) {
		ran = true
		return "", nil
	})
	if ran {
		t.Fatal("run goroutine executed for an invalid parentSession; the fix failed")
	}
	if j.status != Failed {
		t.Fatalf("job status = %q, want %q (artifactErr=%q)", j.status, Failed, j.artifactErr)
	}
	if !strings.Contains(j.artifactErr, "parentSession") {
		t.Fatalf("artifactErr should reference parentSession, got %q", j.artifactErr)
	}
	if j.artifactPath != "" {
		t.Fatalf("artifactPath = %q, want empty (no file should be created)", j.artifactPath)
	}

	afterEntries, err := os.ReadDir(m.tempRoot)
	if err != nil {
		t.Fatalf("read temp root after: %v", err)
	}
	if len(afterEntries) != len(beforeEntries) {
		names := make([]string, 0, len(afterEntries))
		for _, e := range afterEntries {
			names = append(names, e.Name())
		}
		t.Fatalf("temp root gained %d new entries; want no change. New entries: %v", len(afterEntries)-len(beforeEntries), names)
	}
}

// TestStartForSession_RejectsPathTraversalKind ensures a malicious kind
// containing path separators is rejected before any artifact is created.
// See #6932.
func TestStartForSession_RejectsPathTraversalKind(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	ran := false
	j := m.StartForSession("safe-session", "../../../etc", "escape via kind", func(_ context.Context, _ io.Writer) (string, error) {
		ran = true
		return "", nil
	})
	if ran {
		t.Fatal("run goroutine executed for an invalid kind; the fix failed")
	}
	if j.status != Failed {
		t.Fatalf("job status = %q, want %q (artifactErr=%q)", j.status, Failed, j.artifactErr)
	}
	if !strings.Contains(j.artifactErr, "kind") {
		t.Fatalf("artifactErr should reference kind, got %q", j.artifactErr)
	}
	if j.artifactPath != "" {
		t.Fatalf("artifactPath = %q, want empty", j.artifactPath)
	}
}

// TestStartForSession_AcceptsValidInput is a regression guard: the validator
// must not break legitimate callers that use typical session ids and kinds.
func TestStartForSession_AcceptsValidInput(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	ran := false
	release := make(chan struct{})
	j := m.StartForSession("session-20260724-142525abc", "task", "normal call", func(ctx context.Context, _ io.Writer) (string, error) {
		ran = true
		select {
		case <-release:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	j.mu.Lock()
	status := j.status
	artifactErr := j.artifactErr
	artifactPath := j.artifactPath
	j.mu.Unlock()
	if status != Running {
		t.Fatalf("job status = %q, want Running (artifactErr=%q)", status, artifactErr)
	}
	if artifactPath == "" {
		t.Fatal("artifactPath empty; expected a path under the temp root")
	}
	if !strings.HasPrefix(artifactPath, m.tempRoot) {
		t.Fatalf("artifactPath = %q does not start with temp root %q", artifactPath, m.tempRoot)
	}

	// Wait for run to finish and confirm cleanup paths still work.
	close(release)
	res := m.WaitForSession(context.Background(), "session-20260724-142525abc", []string{j.ID}, 5)
	if len(res) != 1 {
		t.Fatalf("WaitForSession returned %d results, want 1", len(res))
	}
	if res[0].Output != "ok" {
		t.Fatalf("run output = %q, want %q", res[0].Output, "ok")
	}
	if !ran {
		t.Fatal("run callback never executed")
	}
}

// TestStartForSession_AcceptsSetActiveSessionPathDir guards against a previous
// regression where a defense-in-depth containment check in openArtifactLocked
// rejected legitimate artifact directories produced by SetActiveSessionPath.
// That path resolves to <root>/<id>.jobs (an absolute directory outside the
// manager's temp root), so a temp-root containment check was a false positive.
// validatePathSegment confines only the temp-root fallback; persistent artifact
// directories come from the trusted transcript path bound by the store layer.
// See #6932.
func TestStartForSession_AcceptsSetActiveSessionPathDir(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "a.jsonl")

	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session-a", sessionPath)

	j := m.StartForSession("session-a", "bash", "regression", func(_ context.Context, _ io.Writer) (string, error) {
		return "ok", nil
	})
	<-j.done
	if j.artifactErr != "" {
		t.Fatalf("artifactErr = %q, want empty (SetActiveSessionPath dir must be accepted)", j.artifactErr)
	}
	if j.artifactPath == "" {
		t.Fatal("artifactPath empty; expected a path under the session dir")
	}
	// The log file must live next to the session transcript.
	wantDir := store.SessionJobsDir(sessionPath)
	if !strings.HasPrefix(j.artifactPath, wantDir) {
		t.Fatalf("artifactPath = %q, want prefix %q", j.artifactPath, wantDir)
	}
}
