package serve

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The dev server proxies a declared list of prefixes to a running kernel and
// answers everything else with the SPA shell. A route missing from that list
// therefore answers with HTML where JSON belongs, which is indistinguishable
// from a broken endpoint — and only in `vite dev`, so tests and production
// stay green while the thing a developer is looking at does not work.
const viteConfig = "../../../desktop/frontend-next/vite.config.ts"

var routesArray = regexp.MustCompile(`(?s)const ROUTES = \[(.*?)\]`)

// notProxied answers which paths the dev server is meant to serve itself: the
// SPA shell, and the assets Vite has in its own tree. Declared, never inferred
// — nothing in a path's spelling says which side should answer it.
func notProxied(path string) bool {
	return path == "/" || strings.HasPrefix(path, "/assets/")
}

// kernelRoutes reads every path this package registers, from the syntax rather
// than from a list someone remembers to update.
func kernelRoutes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HandleFunc" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			// "GET /path" — the method half is the mux's, not the proxy's.
			if _, path, found := strings.Cut(pattern, " "); found {
				out = append(out, path)
			} else {
				out = append(out, pattern)
			}
			return true
		})
	}
	return out
}

// devProxyPrefixes reads the list vite.config.ts declares.
func devProxyPrefixes(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(viteConfig))
	if err != nil {
		t.Skipf("no frontend tree here: %v", err)
	}
	m := routesArray.FindSubmatch(body)
	if m == nil {
		t.Fatal("ROUTES not found in vite.config.ts; this guard is watching nothing")
	}
	var out []string
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(string(m[1]), -1) {
		out = append(out, q[1])
	}
	if len(out) < 10 {
		t.Fatalf("read %d proxy prefixes; this guard is watching nothing", len(out))
	}
	return out
}

// TestEveryRouteReachesTheDevServer holds the two lists together. They are
// compiled by different toolchains and meet only in a browser, so nothing else
// in the tree notices when one of them grows a route.
func TestEveryRouteReachesTheDevServer(t *testing.T) {
	prefixes := devProxyPrefixes(t)
	routes := kernelRoutes(t)
	if len(routes) < 50 {
		t.Fatalf("read %d kernel routes; this guard is watching nothing", len(routes))
	}
	var missing []string
	for _, path := range routes {
		if notProxied(path) {
			continue
		}
		covered := false
		for _, p := range prefixes {
			if strings.HasPrefix(path, p) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("routes the dev server would answer with the SPA shell — add their prefix to ROUTES in %s:\n  %s",
			viteConfig, strings.Join(missing, "\n  "))
	}
}
