package main

import (
	"strings"
	"testing"
)

func orphanFindings(t *testing.T, files map[string]string) []Finding {
	t.Helper()
	scan := newOrphanScan()
	for rel, body := range files {
		src := parseBytes(rel, []byte(body))
		if src == nil {
			t.Fatalf("parse %s", rel)
		}
		scan.observe(src)
	}
	return scan.findings()
}

func names(findings []Finding) string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Msg)
	}
	return strings.Join(out, "\n")
}

func TestOrphanReportsExportedKernelFuncWithNoCaller(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/platform/repair/config.go": "package repair\nfunc RecordHealthyConfig(v string) error { return nil }\n",
	})
	if !strings.Contains(names(got), "RecordHealthyConfig") {
		t.Fatalf("uncalled exported kernel func not reported: %q", names(got))
	}
}

// A doc comment naming the function is what makes an orphan look wired; it is
// not a caller and must not silence the finding.
func TestOrphanIgnoresCommentMentions(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/platform/repair/config.go": "package repair\nfunc RecordHealthyConfig(v string) error { return nil }\n",
		"internal/contract/config/lkg.go":    "package config\n\n// Written by repair.RecordHealthyConfig after a successful boot.\nfunc Path() string { return \"\" }\n",
	})
	if !strings.Contains(names(got), "RecordHealthyConfig") {
		t.Fatal("a comment mention silenced the finding")
	}
}

// Tests are what keep an orphan green; they are not evidence of a caller.
func TestOrphanIgnoresTestCallers(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/platform/repair/config.go":      "package repair\nfunc RecordHealthyConfig(v string) error { return nil }\n",
		"internal/platform/repair/config_test.go": "package repair\nfunc TestX(t *T) { RecordHealthyConfig(\"v\") }\n",
	})
	if !strings.Contains(names(got), "RecordHealthyConfig") {
		t.Fatal("a test caller silenced the finding")
	}
}

// The desktop module is a real caller of internal/; a scan that stopped at one
// module would call its APIs dead.
func TestOrphanCountsCallersInOtherModules(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/platform/repair/config.go": "package repair\nfunc RecordHealthyConfig(v string) error { return nil }\n",
		"desktop/next/main.go":               "package main\nfunc main() { repair.RecordHealthyConfig(\"1\") }\n",
	})
	if strings.Contains(names(got), "RecordHealthyConfig") {
		t.Fatalf("a desktop-module caller was not counted: %q", names(got))
	}
}

func TestOrphanSkipsUnexportedAndNonKernel(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/platform/repair/config.go": "package repair\nfunc recordHealthy(v string) error { return nil }\n",
		"cmd/reasonix/main.go":               "package main\nfunc Unused() {}\n",
	})
	if n := names(got); n != "" {
		t.Fatalf("reported outside the kernel export surface: %q", n)
	}
}

func TestOrphanSkipsTestSupportPackages(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/runtime/agent/testutil/fake.go": "package testutil\nfunc NewFake() int { return 0 }\n",
		"internal/base/testenv/home.go":           "package testenv\nfunc IsolateUserState() {}\n",
		"internal/platform/remote/sshtest/s.go":   "package sshtest\nfunc Start() {}\n",
	})
	if n := names(got); n != "" {
		t.Fatalf("reported a test-support package: %q", n)
	}
}

// A method is reachable through any interface that describes it, including one
// a third-party package owns and calls, and no file here need name it. Proving
// that needs type information this pass does not have, so methods are out of
// scope: guessing wrong here deletes live code.
func TestOrphanSkipsMethods(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/frontend/cli/tui.go": "package cli\ntype chatTUI struct{}\nfunc (chatTUI) Init() int { return 0 }\nfunc (chatTUI) NeverCalled() {}\n",
	})
	if n := names(got); n != "" {
		t.Fatalf("reported a method: %q", n)
	}
}

// The two orphan shapes need different fixes, so the finding has to tell them
// apart: a cross-package test fixture must move, a corpse must go. Deleting the
// first breaks real tests, which is why the message carries the distinction.
func TestOrphanDistinguishesTestFixturesFromCorpses(t *testing.T) {
	got := orphanFindings(t, map[string]string{
		"internal/platform/repair/update.go":      "package repair\nfunc PrepareFileUpdate() {}\nfunc NeverUsed() {}\n",
		"internal/platform/repair/update_test.go": "package repair\nfunc TestX(t *T) { PrepareFileUpdate() }\n",
	})
	msg := names(got)
	if !strings.Contains(msg, "PrepareFileUpdate is referenced only by tests") {
		t.Errorf("a test-referenced orphan was not marked as such: %q", msg)
	}
	if !strings.Contains(msg, "NeverUsed is referenced nowhere, including tests") {
		t.Errorf("an unreferenced orphan was not marked as such: %q", msg)
	}
}
