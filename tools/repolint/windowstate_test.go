package main

import (
	"go/parser"
	"go/token"
	"testing"
)

func windowStateFindings(t *testing.T, src string) []Finding {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return checkWindowState(&sourceFile{rel: "internal/runtime/agent/x.go", fset: fset, file: file})
}

func TestWindowStateOwnersMayTouchIt(t *testing.T) {
	src := `package agent
func (a *contextWindow) f() { a.sess.win.cacheState = "" }
func (r *sessionRuntime) reset() { r.win.reset() }`
	if got := windowStateFindings(t, src); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestWindowStateReadFromTheLoopIsReported(t *testing.T) {
	src := `package agent
func (a *Agent) f() string { return a.sess.win.cacheState }
func g(a *Agent) { a.sess.win.compactionMu.Lock() }`
	got := windowStateFindings(t, src)
	if len(got) != 2 || got[0].Rule != ruleWindowState {
		t.Fatalf("findings = %+v, want one per reading function", got)
	}
}
