package cosplay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLanguageFromPath(t *testing.T) {
	cases := map[string]string{
		"foo.go":       "go",
		"a/b/c.py":     "python",
		"x.js":         "javascript",
		"x.mjs":        "javascript",
		"x.cjs":        "javascript",
		"notes.txt":    "",
		"README.md":    "",
		"noextension":  "",
		"script.sh":    "",
		"App.tsx":      "",
		"model.go.bak": "",
	}
	for path, want := range cases {
		if got := LanguageFromPath(path); got != want {
			t.Errorf("LanguageFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestVerifyFileRunsGo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "add.go")
	code := `package add

// Add returns a + b.
func Add(a, b int) int { return a + b }
`
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	got := VerifyFile(ctx, path, AutoConfig{})
	if got == "" {
		t.Fatal("VerifyFile returned empty for a supported Go file")
	}
	if !strings.Contains(got, "add.go") {
		t.Errorf("summary should mention the file name, got %q", got)
	}
	t.Logf("summary: %s", got)
}

func TestVerifyFileSkipsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := VerifyFile(context.Background(), path, AutoConfig{}); got != "" {
		t.Errorf("VerifyFile on unsupported extension = %q, want empty", got)
	}
}

func TestVerifyFileSkipsUnreadable(t *testing.T) {
	// Supported extension but the file does not exist.
	if got := VerifyFile(context.Background(), filepath.Join(t.TempDir(), "missing.go"), AutoConfig{}); got != "" {
		t.Errorf("VerifyFile on missing file = %q, want empty", got)
	}
}

func TestAutoVerifierMaybeVerifyCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hi.go")
	if err := os.WriteFile(path, []byte(`package hi

// Hi returns a greeting.
func Hi(name string) string { return "hi " + name }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewAutoVerifier(AutoConfig{})
	done := make(chan string, 1)
	// Give the async round plenty of headroom; the callback must fire.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	v.MaybeVerify(ctx, path, func(s string) { done <- s })
	select {
	case s := <-done:
		if !strings.Contains(s, "hi.go") {
			t.Errorf("callback summary = %q, want file name", s)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("MaybeVerify callback never fired")
	}
}

func TestAutoVerifierSkipsUnsupported(t *testing.T) {
	v := NewAutoVerifier(AutoConfig{})
	called := false
	v.MaybeVerify(context.Background(), filepath.Join(t.TempDir(), "x.txt"), func(string) { called = true })
	time.Sleep(50 * time.Millisecond) // no goroutine should have been spawned
	if called {
		t.Fatal("callback fired for unsupported extension")
	}
	if v.HasWork("a.go") == false || v.HasWork("b.txt") == true {
		t.Errorf("HasWork mismatch: go=%v txt=%v", v.HasWork("a.go"), v.HasWork("b.txt"))
	}
}

func TestAutoConfigDefaults(t *testing.T) {
	c := (AutoConfig{}).withDefaults()
	if c.NumTests != 1 || c.MaxRounds != 1 || c.Timeout != 20*time.Second || c.Concurrency != 1 {
		t.Errorf("defaults wrong: %+v", c)
	}
}
