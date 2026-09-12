package main

import (
	"strings"
	"testing"
)

// The default posture must keep the historical --auto spelling: recorded suite
// numbers were produced with that exact command line.
func TestSuitePermissionArgKeepsAutoSpelling(t *testing.T) {
	for _, mode := range []string{"", "auto", "nonsense"} {
		if got := suitePermissionArg(mode); got != "--auto" {
			t.Fatalf("suitePermissionArg(%q) = %q, want --auto", mode, got)
		}
	}
}

func TestSuitePermissionArgYolo(t *testing.T) {
	if got := suitePermissionArg("yolo"); got != "--permission-mode=bypassPermissions" {
		t.Fatalf("suitePermissionArg(yolo) = %q", got)
	}
}

// The posture reaches the agent's command line, and --auto and
// --permission-mode are mutually exclusive in the CLI, so only one may appear.
func TestBuildRunTaskArgsCarriesPosture(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{mode: "auto", want: "--auto"},
		{mode: "yolo", want: "--permission-mode=bypassPermissions"},
	} {
		args := buildRunTaskArgs(suiteConfig{permission: tc.mode}, "/m.json", "", 0, "do it")
		var seen int
		for _, a := range args {
			if a == "--auto" || a == "--permission-mode=bypassPermissions" {
				seen++
				if a != tc.want {
					t.Fatalf("mode %q produced %q, want %q", tc.mode, a, tc.want)
				}
			}
		}
		if seen != 1 {
			t.Fatalf("mode %q produced %d posture flags, want exactly 1: %v", tc.mode, seen, args)
		}
	}
}

// A posture that dropped the approval gate must be visible where the numbers
// are read: two arms otherwise render byte-identical headers.
func TestReportHeaderNamesANonDefaultPosture(t *testing.T) {
	for _, tc := range []struct{ posture, want string }{
		{posture: benchmarkPermissionAuto, want: "## 🤖 Reasonix e2e benchmark (arm `full`)"},
		{posture: benchmarkPermissionYolo, want: "## 🤖 Reasonix e2e benchmark (arm `full` · yolo-permission)"},
	} {
		got, _, _ := strings.Cut(render([]result{{Permission: tc.posture}}), "\n")
		if got != tc.want {
			t.Fatalf("posture %q rendered %q, want %q", tc.posture, got, tc.want)
		}
	}
}
