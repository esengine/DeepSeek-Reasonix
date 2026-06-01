//go:build manual

package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLanguageServers drives each installed mainstream server against a per-
// language fixture under testdata/lsp. Servers not on PATH are skipped. Run with:
//
//	go test -tags manual -run TestLanguageServers -v ./internal/lsp/
func TestLanguageServers(t *testing.T) {
	specs := DefaultSpecs()
	cases := []struct {
		lang, dir, file, callNeedle, symbol string
	}{
		{"go", "golang", "main.go", "_ = greet(", "greet"},
		{"rust", "rust", filepath.Join("src", "main.rs"), "let _ = greet(", "greet"},
		{"typescript", "typescript", "index.ts", "const msg = greet(", "greet"},
		{"python", "python", "main.py", "msg = greet(", "greet"},
		{"bash", "bash", "script.sh", `greet "world"`, "greet"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			spec := specs[tc.lang]
			if _, err := exec.LookPath(spec.Command); err != nil {
				t.Skipf("%s not on PATH (%s)", spec.Command, spec.InstallHint)
			}
			root, err := filepath.Abs(filepath.Join("testdata", "lsp", tc.dir))
			if err != nil {
				t.Fatal(err)
			}
			m := NewManager(root, specs)
			defer m.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
			defer cancel()

			line := findLine(t, root, tc.file, tc.callNeedle)
			def := defWithRetry(t, ctx, m, tc.file, line, tc.symbol, 150*time.Second)
			t.Logf("[%s] definition → %s", tc.lang, def)
			if strings.Contains(def, "no definition") || !strings.Contains(def, filepath.Base(tc.file)) {
				t.Errorf("%s: definition did not resolve into %s: %s", tc.lang, tc.file, def)
			}

			if hov, err := m.Hover(ctx, tc.file, line, tc.symbol); err != nil {
				t.Errorf("%s hover: %v", tc.lang, err)
			} else {
				t.Logf("[%s] hover → %s", tc.lang, oneLine(hov))
			}
			if diag, err := m.Diagnostics(ctx, tc.file); err != nil {
				t.Errorf("%s diagnostics: %v", tc.lang, err)
			} else {
				t.Logf("[%s] diagnostics → %s", tc.lang, oneLine(diag))
			}
		})
	}
}

// defWithRetry polls Definition until it resolves or the budget elapses — some
// servers (rust-analyzer) keep indexing for a while after initialize returns.
func defWithRetry(t *testing.T, ctx context.Context, m *Manager, file string, line int, symbol string, budget time.Duration) string {
	end := time.Now().Add(budget)
	var last string
	for {
		d, err := m.Definition(ctx, file, line, symbol)
		if err != nil {
			// ContentModified means the server is still indexing — retry per LSP §-32801.
			if strings.Contains(err.Error(), "content modified") && time.Now().Before(end) {
				time.Sleep(time.Second)
				continue
			}
			t.Fatalf("definition: %v", err)
		}
		last = d
		if !strings.Contains(d, "no definition") || time.Now().After(end) {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// TestGoplsSmoke drives a real gopls against this module's own source. Run with:
//
//	go test -tags manual -run TestGoplsSmoke -v ./internal/lsp/
func TestGoplsSmoke(t *testing.T) {
	root := moduleRoot(t)
	m := NewManager(root, DefaultSpecs())
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	managerRel := filepath.Join("internal", "lsp", "manager.go")
	callLine := findLine(t, root, managerRel, "err := locate(")

	def, err := m.Definition(ctx, managerRel, callLine, "locate")
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	t.Logf("definition →\n%s", def)
	if !strings.Contains(def, "position.go") {
		t.Errorf("definition of locate should land in position.go, got:\n%s", def)
	}

	hov, err := m.Hover(ctx, managerRel, callLine, "locate")
	if err != nil {
		t.Fatalf("hover: %v", err)
	}
	t.Logf("hover →\n%s", hov)
	if !strings.Contains(hov, "func locate") {
		t.Errorf("hover should show locate's signature, got:\n%s", hov)
	}

	diag, err := m.Diagnostics(ctx, filepath.Join("internal", "lsp", "jsonrpc.go"))
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	t.Logf("diagnostics → %s", diag)
	if !strings.Contains(diag, "no diagnostics") {
		t.Errorf("clean file should have no diagnostics, got: %s", diag)
	}
}

func moduleRoot(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func findLine(t *testing.T, root, rel, needle string) int {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	t.Fatalf("needle %q not found in %s", needle, rel)
	return 0
}
