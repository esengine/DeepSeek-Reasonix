package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
)

// A capability this host implements and does not hand to its hub is one the
// kernel never registers a route for. Nothing fails at compile time, and the
// shell in front of it simply cannot do the thing — which is how remote
// workspaces were missing here while the protocol for them was already green.
func TestTheHostHandsItsHubEveryCapabilityItImplements(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	keys := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HubOptions" {
			return true
		}
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok {
					keys[key.Name] = true
				}
			}
		}
		return false
	})
	if len(keys) == 0 {
		t.Fatal("no hub options found; this guard is watching nothing")
	}
	for _, want := range []string{"Tray", "Asks", "Remote", "DecorateSink", "Grant"} {
		if !keys[want] {
			t.Errorf("the hub is built without %s, so nothing can reach what this host implements for it", want)
		}
	}
}

// The kernel renders text of its own — the built-in slash hints are the ones a
// window shows — and it renders them from a process-wide catalogue that
// something has to point at a language. Every other entry point does it; this
// host did not, and English reached windows set to Chinese for as long as that
// was true. Restored after, because the catalogue outlives the test.
func TestTheHostPointsTheKernelsCatalogueAtTheConfiguredLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	for _, tt := range []struct {
		lang string
		want string
	}{
		{lang: "zh", want: i18n.Chinese.CmdContext},
		{lang: "en", want: i18n.English.CmdContext},
	} {
		if got := resolveKernelLanguage(&config.Config{Language: tt.lang}); got == "" {
			t.Fatalf("language %q resolved to nothing", tt.lang)
		}
		if i18n.M.CmdContext != tt.want {
			t.Errorf("language %q: kernel says %q, want %q", tt.lang, i18n.M.CmdContext, tt.want)
		}
	}
}

// Sweeping a session directory costs a handful of file operations per
// transcript, and the window cannot open until the handshake. A build that
// sweeps inline makes the first launch after the cache went cold wait on
// history it never displays.
func TestTheHostsFirstBuildSweepsSessionsInTheBackground(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Options" {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "CleanupPendingReconciler" {
				if v, ok := kv.Value.(*ast.SelectorExpr); ok && v.Sel.Name == "BackgroundCleanupReconciler" {
					found = true
				}
			}
		}
		return true
	})
	if !found {
		t.Fatal("the host's boot.Options does not set CleanupPendingReconciler to serve.BackgroundCleanupReconciler")
	}
}
