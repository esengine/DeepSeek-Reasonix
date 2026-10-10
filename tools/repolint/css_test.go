package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStyles(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(stylesDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCountImportantIgnoresComments(t *testing.T) {
	css := "a{color:red!important;margin:0 ! important}\n/* b{x:y !important} */\nc{d:e}"
	if got := countImportant(css); got != 2 {
		t.Fatalf("countImportant = %d, want 2", got)
	}
}

// The ratchet's point: a baseline recorded at N fails at N+1 and is quiet at N or below.
func TestCSSRatchetFailsWhenImportantRises(t *testing.T) {
	root := t.TempDir()
	writeStyles(t, root, "studio.css", "a{b:c !important}\nd{e:f !important}\n")
	baseline := baselineFrom(checkCSS(root))

	if _, over := baseline.exceeded(checkCSS(root)); len(over) != 0 {
		t.Fatalf("unchanged tree overran: %v", over)
	}
	writeStyles(t, root, "studio.css", "a{b:c !important}\nd{e:f !important}\ng{h:i !important}\n")
	_, over := baseline.exceeded(checkCSS(root))
	if len(over) == 0 || over[0].Rule != ruleCSSImportant {
		t.Fatalf("a third !important must overrun the baseline, got %v", over)
	}
	writeStyles(t, root, "studio.css", "a{b:c}\n")
	if _, over := baseline.exceeded(checkCSS(root)); len(over) != 0 {
		t.Fatalf("removing declarations must stay free: %v", over)
	}
}

func TestCSSRatchetFailsWhenABudgetedSheetGrows(t *testing.T) {
	root := t.TempDir()
	writeStyles(t, root, "app.css", strings.Repeat("a{b:c}\n", 10))
	baseline := baselineFrom(checkCSS(root))
	writeStyles(t, root, "app.css", strings.Repeat("a{b:c}\n", 11))
	_, over := baseline.exceeded(checkCSS(root))
	if len(over) == 0 || over[0].Rule != ruleCSSSize {
		t.Fatalf("an 11th line must overrun a 10-line budget, got %v", over)
	}
}

func TestCSSSizeBudgetCoversOnlyTheLayeredSheets(t *testing.T) {
	root := t.TempDir()
	writeStyles(t, root, "chart.css", strings.Repeat("a{b:c}\n", 3000))
	if got := checkCSS(root); len(got) != 0 {
		t.Fatalf("an unbudgeted sheet must not be weighed by length: %v", got)
	}
}

func TestStrayFilesAreFlagged(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Settings.tsx-e", "x.go.orig", "ok.tsx", "name-eel.ts"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := checkStrayFiles(root)
	if len(got) != 2 {
		t.Fatalf("findings = %v, want the -e and .orig files", got)
	}
}
