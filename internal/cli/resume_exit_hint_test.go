package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

func TestResumeExitHintEmpty(t *testing.T) {
	if got := resumeExitHint(""); got != "" {
		t.Fatalf("resumeExitHint(\"\") = %q, want empty", got)
	}
}

func TestResumeExitHintFormatsSessionID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20260718-101530.123456789-deepseek-v3.jsonl")
	id := agent.BranchID(path)
	got := resumeExitHint(path)
	if want := fmt.Sprintf(i18n.M.ResumeExitHintFmt, id); got != want {
		t.Fatalf("resumeExitHint(%q) = %q, want %q", path, got, want)
	}
	if !strings.Contains(got, "reasonix --resume "+id) {
		t.Fatalf("resumeExitHint(%q) = %q, missing resume command with session id", path, got)
	}
}

func TestActiveSessionPathPrefersFinalController(t *testing.T) {
	launch := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(launch.Close)
	active := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(active.Close)
	launch.EnsureSessionPath()
	active.EnsureSessionPath()

	if got := activeSessionPath(chatTUI{ctrl: active}, launch); got != active.SessionPath() {
		t.Fatalf("activeSessionPath with active final ctrl = %q, want %q", got, active.SessionPath())
	}
	if got := activeSessionPath(chatTUI{ctrl: nil}, launch); got != launch.SessionPath() {
		t.Fatalf("activeSessionPath with nil final ctrl = %q, want %q", got, launch.SessionPath())
	}
	if got := activeSessionPath(nil, launch); got != launch.SessionPath() {
		t.Fatalf("activeSessionPath with non-chatTUI final = %q, want %q", got, launch.SessionPath())
	}
}

func TestExitWithResumeHintPrintsActiveSession(t *testing.T) {
	launch := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(launch.Close)
	active := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(active.Close)
	launch.EnsureSessionPath()
	active.EnsureSessionPath()
	want := fmt.Sprintf(i18n.M.ResumeExitHintFmt, agent.BranchID(active.SessionPath()))

	out := captureStdout(t, func() {
		if code := exitWithResumeHint(chatTUI{ctrl: active}, launch); code != 0 {
			t.Fatalf("exitWithResumeHint = %d, want 0", code)
		}
	})
	if !strings.Contains(out, want) {
		t.Fatalf("exitWithResumeHint printed %q, want it to contain %q", out, want)
	}
}

func TestExitWithResumeHintSilentWithoutSession(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(ctrl.Close)

	out := captureStdout(t, func() {
		if code := exitWithResumeHint(nil, ctrl); code != 0 {
			t.Fatalf("exitWithResumeHint = %d, want 0", code)
		}
	})
	if out != "" {
		t.Fatalf("exitWithResumeHint printed %q for an empty session path, want nothing", out)
	}
}
