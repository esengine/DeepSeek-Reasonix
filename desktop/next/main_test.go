package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The shell decides per request whether a path is the kernel's or the SPA's. A
// kernel path that falls through answers with index.html, and the frontend then
// fails parsing HTML as JSON — a failure that reads like a broken endpoint.
func TestIsAPIPathCoversResourceFamilies(t *testing.T) {
	for _, p := range []string{
		"/status", "/mcp", "/mcp/", "/mcp/reconnect", "/mcp/enabled",
		"/skills", "/skills/enabled", "/inbox/items", "/workspaces",
		"/hooks", "/hooks/dry-run", "/memory", "/memory/forget",
		"/network", "/network/diagnose",
	} {
		if !isAPIPath(p) {
			t.Errorf("isAPIPath(%q) = false, want true", p)
		}
	}
	// Anything not claimed here has to reach the assets, or a deep link stops
	// rendering the app.
	for _, p := range []string{"/", "/sessions/abc", "/assets/app.js", "/mcpx", "/skillset"} {
		if isAPIPath(p) {
			t.Errorf("isAPIPath(%q) = true, want false", p)
		}
	}
}

// The list above is hand-maintained, and hand-maintained lists drift: the hooks,
// memory and network panels shipped reading endpoints this shell never routed,
// so all three came up empty in the app and worked in the browser. Reading the
// paths back out of the client is what makes the next one fail here instead.
func TestEveryPathTheFrontendCallsIsRouted(t *testing.T) {
	const client = "../frontend-next/src/port/sse.ts"
	src, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	// Anchored on the call, so this.base + "/plugins/" + name + "/export" does
	// not read as a request for "/export". The type argument is not cosmetic:
	// without it this.get<T>("/x") matched nothing — 24 of 60 paths went unseen.
	literal := regexp.MustCompile(`this\.(?:\w+(?:<[^()"]*>)?\(|base \+ )\s*"(/[a-z0-9][a-z0-9/-]*)"`)
	seen := map[string]bool{}
	for _, m := range literal.FindAllStringSubmatch(string(src), -1) {
		// A trailing slash means an id follows, so probe the family rather
		// than the bare prefix, which isAPIPath deliberately rejects.
		path := m[1]
		if strings.HasSuffix(path, "/") {
			path += "x"
		}
		// The replay endpoint is the shell's own; it is answered by the
		// middleware before the API ever sees it.
		if seen[path] || path == replayPath {
			continue
		}
		seen[path] = true
		if !isAPIPath(path) {
			t.Errorf("%s calls %q but the shell routes it to the assets, so it answers index.html", client, path)
		}
	}
	if len(seen) < 20 {
		t.Fatalf("only found %d paths in %s — the extraction broke, not the routing", len(seen), client)
	}
}
