package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/ablation"
)

// The harness prints an unmangled local image key but pulls a mangled one; the
// two differ and only the mangled form exists in the registry.
func TestSwebenchImageManglesDoubleUnderscore(t *testing.T) {
	got := swebenchImage("swebench", "psf__requests-2317")
	want := "swebench/sweb.eval.x86_64.psf_1776_requests-2317:latest"
	if got != want {
		t.Fatalf("image = %q, want %q", got, want)
	}
	if got := swebenchImage("swebench", "pylint-dev__pylint-7080"); !strings.Contains(got, "pylint-dev_1776_pylint-7080") {
		t.Fatalf("hyphenated org mangled wrong: %q", got)
	}
}

func TestTestbedShellActivatesTheInstanceCondaEnv(t *testing.T) {
	got := testbedShell("git diff")
	if len(got) != 3 || got[0] != "bash" || got[1] != "-lc" {
		t.Fatalf("shell wrapper = %v", got)
	}
	for _, want := range []string{"/opt/miniconda3/bin/activate", "conda activate testbed", "cd /testbed", "git diff"} {
		if !strings.Contains(got[2], want) {
			t.Errorf("command missing %q: %s", want, got[2])
		}
	}
}

func TestSwebenchPromptWithholdsTheAnswerKey(t *testing.T) {
	prompt := swebenchPrompt(swebenchInstance{
		InstanceID: "psf__requests-2317",
		Problem:    "method = builtin_str(method) breaks binary strings",
	})
	if !strings.Contains(prompt, "builtin_str(method)") {
		t.Fatal("the issue text must reach the agent")
	}
	for _, leak := range []string{"FAIL_TO_PASS", "PASS_TO_PASS", "test_patch"} {
		if strings.Contains(prompt, leak) {
			t.Errorf("prompt leaks the answer key: %s", leak)
		}
	}
	if !strings.Contains(prompt, "do not modify any test file") {
		t.Error("the no-test-edit rule must be stated; the grader replaces test files anyway")
	}
}

func TestEncodePredictionsWritesOneHarnessRecordPerLine(t *testing.T) {
	out, err := encodePredictions("reasonix", map[string]string{
		"a__a-1": "diff --git a/x b/x\n",
		"b__b-2": "diff --git a/y b/y\n",
	}, []string{"a__a-1", "missing__missing-9", "b__b-2"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (an instance with no patch is skipped, not emitted empty)", len(lines))
	}
	for _, want := range []string{`"instance_id":"a__a-1"`, `"model_name_or_path":"reasonix"`, `"model_patch":"diff --git a/x b/x\n"`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("first record missing %q: %s", want, lines[0])
		}
	}
}

func TestGradedClassNeverGuessesForAnUnmentionedInstance(t *testing.T) {
	report := swebenchReport{
		ResolvedIDs:   []string{"a__a-1"},
		UnresolvedIDs: []string{"b__b-2"},
		ErrorIDs:      []string{"c__c-3"},
		EmptyPatchIDs: []string{"d__d-4"},
		IncompleteIDs: []string{"f__f-6"},
	}
	for id, want := range map[string]string{
		"a__a-1": "solved",
		"b__b-2": "wrong_patch",
		"c__c-3": "grader_error",
		"d__d-4": "no_patch",
		"f__f-6": "eval_timeout",
		"e__e-5": "ungraded",
	} {
		if got := report.gradedClass(id); got != want {
			t.Errorf("class(%s) = %q, want %q", id, got, want)
		}
	}
}

// Captured from a real run on 2026-08-04: swebench 4.1.0, gold predictions,
// run_id goldpylint. Pins the field names and the report path we depend on.
func TestSwebenchReportParsesTheHarnessSummary(t *testing.T) {
	const captured = `{"total_instances":1,"submitted_instances":500,"completed_instances":1,
	"resolved_instances":1,"unresolved_instances":0,"empty_patch_instances":0,"error_instances":0,
	"completed_ids":["pylint-dev__pylint-7080"],"incomplete_ids":[],"empty_patch_ids":[],
	"resolved_ids":["pylint-dev__pylint-7080"],"unresolved_ids":[],"error_ids":[],"schema_version":2}`

	var report swebenchReport
	if err := json.Unmarshal([]byte(captured), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := report.gradedClass("pylint-dev__pylint-7080"); got != "solved" {
		t.Fatalf("gold patch classified as %q, want solved", got)
	}
	if got := swebenchReportPath("gold", "goldpylint"); got != "gold.goldpylint.json" {
		t.Fatalf("report path = %q, want gold.goldpylint.json", got)
	}
}

// Issue text arrives verbatim from GitHub and routinely contains quotes,
// backticks, $ and newlines. It becomes one argv element inside a `bash -lc`
// string, so a quoting slip would let a problem statement run commands.
func TestShellQuoteAllContainsHostileIssueText(t *testing.T) {
	hostile := "it's broken; `rm -rf /`; $(whoami)\n\"quoted\" && echo pwned"
	got := shellQuoteAll([]string{"run", hostile})
	if !strings.HasPrefix(got, "'run' '") || !strings.HasSuffix(got, "'") {
		t.Fatalf("every element must be single-quoted: %s", got)
	}
	// Inside single quotes the shell expands nothing, so the only way out is an
	// unescaped apostrophe: every one in the payload must have been rewritten.
	if strings.Count(got, `'\''`) != strings.Count(hostile, "'") {
		t.Fatalf("apostrophes not all escaped: %s", got)
	}
}

func TestSwebenchAgentArgsKeepTheControlArmClean(t *testing.T) {
	got := swebenchAgentArgs("/tmp/m.json", "e2e", benchmarkProfileBaseline, "auto", ablation.Set{}, 60, "fix it")
	want := []string{"run", "--permission-mode=auto", "--metrics", "/tmp/m.json", "--model", "e2e", "--max-steps", "60", "fix it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("control args = %v, want %v", got, want)
	}
	ablated := swebenchAgentArgs("/tmp/m.json", "", benchmarkProfileBaseline, "auto", ablation.New(ablation.Evidence), 0, "fix it")
	if !reflect.DeepEqual(ablated, []string{"run", "--permission-mode=auto", "--metrics", "/tmp/m.json", "--ablate", "evidence", "fix it"}) {
		t.Fatalf("ablated args = %v", ablated)
	}
}

// The two postures must differ in exactly one argument. If anything else moved
// between arms, the published delta would not isolate the permission gate.
func TestPermissionPostureIsTheOnlyDifferenceBetweenArms(t *testing.T) {
	a := swebenchAgentArgs("/m.json", "e2e", benchmarkProfileBaseline, "auto", ablation.Set{}, 60, "fix it")
	b := swebenchAgentArgs("/m.json", "e2e", benchmarkProfileBaseline, "yolo", ablation.Set{}, 60, "fix it")
	if len(a) != len(b) {
		t.Fatalf("arms differ in argument count: %v vs %v", a, b)
	}
	diffs := 0
	for i := range a {
		if a[i] != b[i] {
			diffs++
		}
	}
	if diffs != 1 || a[1] != "--permission-mode=auto" || b[1] != "--permission-mode=bypassPermissions" {
		t.Fatalf("arms must differ only in the posture flag: %v vs %v", a, b)
	}
	if _, err := permissionFlag("bypass"); err == nil {
		t.Fatal("an unknown posture must fail loudly rather than silently running unattended")
	}
}
