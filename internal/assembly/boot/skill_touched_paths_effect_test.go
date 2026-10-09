package boot

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func buildTouchedPathsController(t *testing.T, kind string, calls []scriptedCall) (*control.Controller, string, *scriptedCallProvider) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	p := &scriptedCallProvider{calls: calls}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	writeFile(t, dir, "src/a.go", "package src\n")
	writeFile(t, dir, "docs/readme.md", "# hi\n")
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	return ctrl, dir, p
}

// What the host saw touched is fed by the real tools through the real build:
// a read that succeeded counts; a read that failed, and a file outside the
// workspace, do not.
func TestEffectCompletedReadsFeedTheSkillPathSet(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "elsewhere.go")
	if err := os.WriteFile(outside, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl, _, _ := buildTouchedPathsController(t, "touched-effect-reads", []scriptedCall{
		{"read_file", `{"path":"src/a.go"}`},
		{"read_file", `{"path":"src/missing.go"}`},
		{"read_file", `{"path":` + strconv.Quote(outside) + `}`},
		{"read_file", `{"path":"docs/readme.md"}`},
	})
	if err := ctrl.Run(context.Background(), "look around"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := ctrl.TouchedPaths(), []string{"docs/readme.md", "src/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TouchedPaths = %q, want %q", got, want)
	}
}

// A sub-agent's reads belong to its own context, not to the session that
// delegated; its parent only sees the answer it returns.
func TestEffectSubagentReadsDoNotFeedTheSessionPathSet(t *testing.T) {
	ctrl, _, prov := buildTouchedPathsController(t, "touched-effect-sub", []scriptedCall{
		{"task", `{"prompt":"read src/a.go and report"}`},
		{"read_file", `{"path":"src/a.go"}`},
	})
	if err := ctrl.Run(context.Background(), "delegate"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prov.mu.Lock()
	rounds := prov.round
	prov.mu.Unlock()
	if rounds < 3 {
		t.Fatalf("the sub-agent never ran its read (%d model rounds)", rounds)
	}
	if got := ctrl.TouchedPaths(); len(got) != 0 {
		t.Fatalf("a sub-agent's read reached the session: %q", got)
	}
}

// Resuming a saved conversation rebuilds the set from its transcript rather
// than from anything stored beside it.
func TestEffectResumeRebuildsTheSkillPathSetFromTheTranscript(t *testing.T) {
	ctrl, dir, _ := buildTouchedPathsController(t, "touched-effect-resume", []scriptedCall{
		{"read_file", `{"path":"src/a.go"}`},
	})
	if err := ctrl.Run(context.Background(), "read it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := ctrl.TouchedPaths(), []string{"src/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("before resume: %q, want %q", got, want)
	}
	saved := sessionstore.NewSession("BASE")
	for _, m := range ctrl.History() {
		saved.Add(m)
	}
	fresh, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer fresh.Close()
	if got := fresh.TouchedPaths(); len(got) != 0 {
		t.Fatalf("a new session started with %q", got)
	}
	if err := fresh.Resume(saved, filepath.Join(dir, "resumed.jsonl")); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got, want := fresh.TouchedPaths(), []string{"src/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after resume: %q, want %q", got, want)
	}
	if err := fresh.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := fresh.TouchedPaths(); len(got) != 0 {
		t.Fatalf("/new kept %q", got)
	}
}

func grepCall(args string) scriptedCall {
	return scriptedCall{"use_capability", `{"action":"call","capability_id":"tool:grep","arguments":` + args + `}`}
}

// A touch is "named and succeeded", not "content reached the model": a search
// over a directory or the whole workspace names no file, a search of one named
// file counts even when nothing in it matched.
func TestEffectGrepRecordsOnlyAFileItWasPointedAt(t *testing.T) {
	ctrl, _, p := buildTouchedPathsController(t, "touched-effect-grep", []scriptedCall{
		grepCall(`{"pattern":"package","path":"src"}`),
		grepCall(`{"pattern":"package"}`),
		grepCall(`{"pattern":"zzz-no-such-text","path":"docs/readme.md"}`),
		grepCall(`{"pattern":"package","path":"src/a.go"}`),
	})
	if err := ctrl.Run(context.Background(), "search"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := range 4 {
		if res := p.resultOf(i); res == "" || strings.Contains(res, "error") {
			t.Fatalf("grep call %d did not complete: %q", i, res)
		}
	}
	want := []string{"docs/readme.md", "src/a.go"}
	if got := ctrl.TouchedPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("TouchedPaths = %q, want %q", got, want)
	}
	saved := sessionstore.NewSession("BASE")
	for _, m := range ctrl.History() {
		saved.Add(m)
	}
	fresh, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer fresh.Close()
	if err := fresh.Resume(saved, filepath.Join(t.TempDir(), "resumed.jsonl")); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := fresh.TouchedPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after resume TouchedPaths = %q, want the live set %q", got, want)
	}
}

// Resume resolves every replayed path against the disk as it is now, so a link
// retargeted between sessions changes what the transcript yields.
func TestEffectResumeResolvesSymlinksAgainstTheCurrentDisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}
	ctrl, dir, _ := buildTouchedPathsController(t, "touched-effect-link", []scriptedCall{
		{"read_file", `{"path":"link/a.go"}`},
	})
	if err := os.Symlink(filepath.Join(dir, "src"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run(context.Background(), "read it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := ctrl.TouchedPaths(), []string{"link/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live: %q, want %q", got, want)
	}
	saved := sessionstore.NewSession("BASE")
	for _, m := range ctrl.History() {
		saved.Add(m)
	}
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	fresh, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer fresh.Close()
	if err := fresh.Resume(saved, filepath.Join(dir, "resumed.jsonl")); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := fresh.TouchedPaths(); len(got) != 0 {
		t.Fatalf("a link now pointing outside the workspace still counted: %q", got)
	}
}
