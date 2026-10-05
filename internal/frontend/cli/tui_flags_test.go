package cli

import (
	"slices"
	"testing"

	"reasonix/internal/session/control"
)

func parsedTUIFlags(t *testing.T, args ...string) *tuiFlags {
	t.Helper()
	f := newTUIFlags()
	if err := f.fs.Parse(normalizeOptionalResumeArg(args)); err != nil {
		t.Fatal(err)
	}
	return f
}

// A 1.x command line sets the same posture in the terminal UI: --yolo and its
// long spelling skip approvals, acceptEdits allows the write tools, and no
// flag at all leaves the configured default alone.
func TestTUIFlagsSetThePermissionPosture(t *testing.T) {
	for _, flag := range []string{"--yolo", "--dangerously-skip-permissions"} {
		mode, _, set, err := parsedTUIFlags(t, flag).permissions()
		if err != nil || !set || mode.approval != control.ToolApprovalYolo {
			t.Fatalf("%s: mode %+v set=%v err=%v", flag, mode, set, err)
		}
	}
	if _, _, _, err := parsedTUIFlags(t, "--yolo", "--permission-mode", "ask").permissions(); err == nil {
		t.Fatal("--yolo with --permission-mode was accepted")
	}
	mode, allowed, set, err := parsedTUIFlags(t, "--permission-mode", "acceptEdits", "--allowed-tools", "Bash(git status)").permissions()
	if err != nil || !set || mode.approval != control.ToolApprovalAsk || !slices.Contains(allowed, "write_file") || !slices.Contains(allowed, "Bash(git status)") {
		t.Fatalf("acceptEdits: %+v %v set=%v err=%v", mode, allowed, set, err)
	}
	if mode, _, set, err := parsedTUIFlags(t, "--permission-mode", "plan").permissions(); err != nil || !mode.plan {
		t.Fatalf("plan: %+v set=%v err=%v", mode, set, err)
	}
	if _, _, set, err := parsedTUIFlags(t).permissions(); err != nil || set {
		t.Fatalf("no flag named a mode: set=%v err=%v", set, err)
	}
}

func TestTUIFlagsKeepSessionOptions(t *testing.T) {
	f := parsedTUIFlags(t, "-r", "abc", "--effort", "high", "--add-dir", "/tmp/x", "--max-steps", "7", "fix", "it")
	if *f.resume != "abc" || *f.effortOverride() != "high" || f.addDirs[0] != "/tmp/x" || *f.maxSteps != 7 {
		t.Fatalf("flags = resume %q effort %v dirs %v steps %d", *f.resume, f.effortOverride(), f.addDirs, *f.maxSteps)
	}
	if got := f.fs.Args(); len(got) != 2 || got[0] != "fix" {
		t.Fatalf("prompt args = %v", got)
	}
	if parsedTUIFlags(t).effortOverride() != nil {
		t.Fatal("an absent --effort overrode the configured effort")
	}
	if *parsedTUIFlags(t, "-r").resume != resumePickerSentinel {
		t.Fatal("bare -r does not open the picker")
	}
}
