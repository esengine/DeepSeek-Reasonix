package codegraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

// TestE2ECodegraphMCP drives the whole integration against a real CodeGraph
// bundle: index a fixture project, connect via the MCP client pinned to that
// project (Spec.Dir), and actually call codegraph_search. It is gated on
// REASONIX_CODEGRAPH_E2E so the normal `go test ./...` skips it (no network, no
// external binary), yet it still compiles every build so it can't bit-rot.
//
// Run it with `make e2e-codegraph` (fetches the matching bundle), or manually:
//
//	REASONIX_CODEGRAPH_E2E=1 REASONIX_CODEGRAPH_BIN=/path/to/codegraph \
//	  go test ./internal/codegraph/ -run E2E -v -count=1
//
// With REASONIX_CODEGRAPH_BIN unset it falls back to Resolve("") (bundle / PATH).
func TestE2ECodegraphMCP(t *testing.T) {
	if os.Getenv("REASONIX_CODEGRAPH_E2E") == "" {
		t.Skip("set REASONIX_CODEGRAPH_E2E=1 to run the CodeGraph MCP end-to-end test")
	}
	bin := os.Getenv("REASONIX_CODEGRAPH_BIN")
	if bin == "" {
		var ok bool
		if bin, ok = Resolve(""); !ok {
			t.Fatal("REASONIX_CODEGRAPH_E2E is set but no codegraph binary found — set REASONIX_CODEGRAPH_BIN to the launcher path")
		}
	}
	t.Logf("codegraph binary: %s", bin)

	// A fixture project carrying a known symbol the search must surface.
	root := t.TempDir()
	src := "package demo\n\n// Greet builds a greeting.\nfunc Greet(name string) string { return \"hi \" + name }\n"
	if err := os.WriteFile(filepath.Join(root, "greet.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 1) Initialise .codegraph/ (fast, no indexing — serve's daemon does that).
	if err := EnsureInit(ctx, bin, root); err != nil {
		t.Fatalf("EnsureInit: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, ".codegraph")); err != nil || !fi.IsDir() {
		t.Fatalf(".codegraph was not created by EnsureInit: %v", err)
	}

	// 2) Connect through the real MCP client, pinned to the project root via Dir —
	//    the same wiring boot uses for the built-in server.
	host, tools, err := plugin.StartAll(ctx, []plugin.Spec{{
		Name:              "codegraph",
		Command:           bin,
		Args:              []string{"serve", "--mcp"},
		Dir:               root,
		ReadOnlyToolNames: ReadOnlyToolNames(),
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	if len(tools) == 0 {
		t.Fatal("codegraph exposed no MCP tools")
	}

	// 3) Locate the search tool and actually invoke it through Tool.Execute.
	var search tool.Tool
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name())
		if strings.Contains(tl.Name(), "codegraph_search") {
			search = tl
		}
	}
	if search == nil {
		t.Fatalf("no codegraph_search tool among %v", names)
	}
	if !search.ReadOnly() {
		t.Fatalf("codegraph_search should be read-only under the built-in CodeGraph override; tools=%v", names)
	}

	// serve indexes in the background, so poll the search for a few seconds until
	// the known symbol surfaces (rather than assuming it is ready at handshake).
	var out string
	deadline := time.Now().Add(15 * time.Second)
	for {
		var err error
		if out, err = search.Execute(ctx, json.RawMessage(`{"query":"Greet"}`)); err != nil {
			t.Fatalf("codegraph_search Execute: %v\nschema: %s", err, search.Schema())
		}
		if strings.Contains(out, "Greet") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("codegraph_search never surfaced the known symbol Greet within 15s:\n%s", out)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("e2e ok — %d tools (%v); search surfaced Greet", len(tools), names)
}
