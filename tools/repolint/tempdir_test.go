package main

import "testing"

func tempDirFindings(t *testing.T, rel, src string) []Finding {
	t.Helper()
	file := parseBytes(rel, []byte(src))
	if file == nil || file.file == nil {
		t.Fatalf("%s did not parse", rel)
	}
	return checkTempDir(file)
}

func TestBareTempDirInATestIsReported(t *testing.T) {
	src := `package x
import "testing"
func TestA(t *testing.T) { _ = t.TempDir() }
func BenchmarkB(b *testing.B) { _ = b.TempDir() }
type fx struct{ t *testing.T }
func (f *fx) dir() string { return f.t.TempDir() }`
	got := tempDirFindings(t, "internal/state/x/x_test.go", src)
	if len(got) != 3 || got[0].Rule != ruleTempDir {
		t.Fatalf("findings = %+v, want the test, the benchmark and the field receiver", got)
	}
}

func TestTempDirLeavesTheOSTempDirAndTheHelperAlone(t *testing.T) {
	src := `package x
import (
	"os"
	"testing"
	"reasonix/internal/base/testenv"
)
func TestA(t *testing.T) { _ = os.TempDir(); _ = testenv.TempDir(t) }`
	if got := tempDirFindings(t, "internal/state/x/x_test.go", src); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestTempDirFollowsARenamedOSImport(t *testing.T) {
	src := `package x
import sys "os"
func TestA() { _ = sys.TempDir() }`
	if got := tempDirFindings(t, "internal/state/x/x_test.go", src); len(got) != 0 {
		t.Fatalf("findings = %+v, want the renamed os import left alone", got)
	}
}

func TestTempDirOnlyReadsTests(t *testing.T) {
	src := `package x
func f(t interface{ TempDir() string }) string { return t.TempDir() }`
	if got := tempDirFindings(t, "internal/state/x/x.go", src); len(got) != 0 {
		t.Fatalf("findings = %+v, want a non-test file left alone", got)
	}
}

func TestTempDirHelperPackageMayUseTheStandardOne(t *testing.T) {
	src := `package testenv
import "testing"
func TestTempDir(t *testing.T) { _ = t.TempDir() }`
	if got := tempDirFindings(t, "internal/base/testenv/tempdir_test.go", src); len(got) != 0 {
		t.Fatalf("findings = %+v, want the helper's own test left alone", got)
	}
}
