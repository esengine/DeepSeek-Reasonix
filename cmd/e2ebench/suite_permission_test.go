package main

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/ablation"
)

// postureFlags returns every --permission-mode= argument in an argv, so a test
// can prove the posture reaches an invocation exactly once.
func postureFlags(args []string) []string {
	var seen []string
	for _, a := range args {
		if strings.HasPrefix(a, "--permission-mode=") {
			seen = append(seen, a)
		}
	}
	return seen
}

// The posture reaches the agent's command line exactly once: the CLI rejects a
// run that carries two of them.
func TestBuildRunTaskArgsCarriesPosture(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{mode: "", want: "--permission-mode=workspace-write"},
		{mode: benchmarkPermissionDefault, want: "--permission-mode=workspace-write"},
		{mode: "read-only", want: "--permission-mode=read-only"},
		{mode: "danger-full-access", want: "--permission-mode=danger-full-access"},
	} {
		args := buildRunTaskArgs(suiteConfig{permission: tc.mode}, "/m.json", "", 0, "do it")
		if seen := postureFlags(args); len(seen) != 1 || seen[0] != tc.want {
			t.Fatalf("mode %q produced posture flags %v, want exactly [%s]: %v", tc.mode, seen, tc.want, args)
		}
	}
}

// The ungraded warm primer runs in the same task workdir the graded run is
// scored in. A primer under a posture the caller did not select can mutate the
// fixtures outside the recorded permission axis, so both invocations have to
// carry the selected preset — and carry it exactly once.
func TestWarmPrefixPrimerCarriesTheSelectedPosture(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{mode: "", want: "--permission-mode=workspace-write"},
		{mode: benchmarkPermissionDefault, want: "--permission-mode=workspace-write"},
		{mode: "read-only", want: "--permission-mode=read-only"},
		{mode: "danger-full-access", want: "--permission-mode=danger-full-access"},
	} {
		cfg := suiteConfig{permission: tc.mode}
		primer := buildWarmPrefixArgs(cfg)
		if seen := postureFlags(primer); len(seen) != 1 || seen[0] != tc.want {
			t.Fatalf("mode %q: primer posture flags %v, want exactly [%s]: %v", tc.mode, seen, tc.want, primer)
		}
		graded := buildRunTaskArgs(cfg, "/m.json", "", 0, "do it")
		if seen := postureFlags(graded); len(seen) != 1 || seen[0] != tc.want {
			t.Fatalf("mode %q: graded posture flags %v, want exactly [%s]: %v", tc.mode, seen, tc.want, graded)
		}
		if primer[0] != "run" {
			t.Fatalf("mode %q: primer is not a run invocation: %v", tc.mode, primer)
		}
	}
}

// The primer exists to warm the prefix the graded run will send, so every flag
// that rewrites that prefix has to appear in both argv.
func TestWarmPrefixPrimerMatchesTheGradedPrefixShape(t *testing.T) {
	cfg := suiteConfig{permission: "read-only", model: "e2e", effort: "low", arm: ablation.New(ablation.Evidence)}
	primer := buildWarmPrefixArgs(cfg)
	graded := buildRunTaskArgs(cfg, "/m.json", "", 12, "do it")
	for _, want := range []string{"--permission-mode=read-only", "--model", "e2e", "--effort", "low", "--ablate", "evidence"} {
		if !slices.Contains(primer, want) {
			t.Fatalf("primer %v is missing prefix-shaping argument %q", primer, want)
		}
		if !slices.Contains(graded, want) {
			t.Fatalf("graded %v is missing prefix-shaping argument %q", graded, want)
		}
	}
	// The primer stays ungraded and one step: its cost is deliberately untracked.
	if slices.Contains(primer, "--metrics") || slices.Contains(primer, "--trajectory") {
		t.Fatalf("primer must not be metered: %v", primer)
	}
	if i := slices.Index(primer, "--max-steps"); i < 0 || primer[i+1] != "1" {
		t.Fatalf("primer must run exactly one step: %v", primer)
	}
}

func TestPermissionFlagRejectsAnUnknownPreset(t *testing.T) {
	for _, mode := range []string{"auto", "yolo", "bypassPermissions", "nonsense"} {
		if _, err := permissionFlag(mode); err == nil {
			t.Fatalf("permissionFlag(%q) accepted an unknown preset", mode)
		}
	}
}

// A posture that dropped the approval gate must be visible where the numbers
// are read: two arms otherwise render byte-identical headers.
func TestReportHeaderNamesANonDefaultPosture(t *testing.T) {
	for _, tc := range []struct{ posture, want string }{
		{posture: benchmarkPermissionDefault, want: "## 🤖 Reasonix e2e benchmark (arm `full`)"},
		{posture: "danger-full-access", want: "## 🤖 Reasonix e2e benchmark (arm `full` · danger-full-access-permission)"},
	} {
		got, _, _ := strings.Cut(render([]result{{Permission: tc.posture}}), "\n")
		if got != tc.want {
			t.Fatalf("posture %q rendered %q, want %q", tc.posture, got, tc.want)
		}
	}
}

// A run whose first task is skipped — no anchor seed authored, or the budget
// already spent — must still report the posture the graded rows ran under.
func TestReportHeaderNamesThePostureBehindALeadingSkippedRow(t *testing.T) {
	results := runSuite(suiteConfig{permission: "danger-full-access", anchor: "correct", budget: 1}, []task{{ID: "seedless"}})
	if len(results) != 1 || !results[0].Skipped {
		t.Fatalf("want one skipped result, got %+v", results)
	}
	if results[0].Permission != "danger-full-access" {
		t.Fatalf("skipped result dropped the permission axis: %+v", results[0])
	}
	results = append(results, result{Permission: "danger-full-access", Passed: true})
	got, _, _ := strings.Cut(render(results), "\n")
	want := "## 🤖 Reasonix e2e benchmark (arm `full` · danger-full-access-permission)"
	if got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
}

// Older report JSON carries the axes only on rows that actually ran, so the
// header is derived from the first row that has a value, not from row zero.
func TestReportHeaderIgnoresALeadingRowWithoutAxes(t *testing.T) {
	results := []result{{Skipped: true}, {Arm: "no-evidence", CacheArm: "warm", Permission: "read-only"}}
	got, _, _ := strings.Cut(render(results), "\n")
	want := "## 🤖 Reasonix e2e benchmark (arm `no-evidence` · warm-cache · read-only-permission)"
	if got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
}
