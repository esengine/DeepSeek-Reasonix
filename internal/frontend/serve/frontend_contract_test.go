package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Both sides write these paths as strings, and nothing compares them until a
// request does — the compiler and tsc each see only a literal, so a path this
// package never registered answers 404. One direction: a route with no caller
// is not a fault, since the CLI and the setup page reach some of them, while a
// call with no route always is.
func TestFrontendCallsOnlyPathsThisPackageServes(t *testing.T) {
	front := filepath.Join("..", "..", "..", "desktop", "frontend-next", "src", "port")
	entries, err := os.ReadDir(front)
	if err != nil {
		t.Skipf("frontend port layer unavailable: %v", err)
	}

	served := servedRoutes(t)
	call := regexp.MustCompile(`(?:\.(?:get|post|post0|del|patch)(?:<[^>]*>)?\(\s*|fetch\(\s*)(?:this\.base \+ )?"(/[^"]*)"`)

	var checked int
	var missing []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".test.ts") || strings.HasPrefix(name, "mock") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(front, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			path, _, _ := strings.Cut(m[1], "?")
			if path == "" {
				continue
			}
			checked++
			if !servedBy(served, path) {
				missing = append(missing, name+" calls "+path)
			}
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)
		t.Errorf("no route answers these:\n\t%s", strings.Join(missing, "\n\t"))
	}
	// Either side can be restructured out of the shape these patterns read. A
	// run that compared almost nothing is the drift itself, not a clean sweep.
	if checked < 60 {
		t.Fatalf("only %d calls were read; the frontend no longer states its paths where this can see them", checked)
	}
	if len(served) < 60 {
		t.Fatalf("only %d routes were read; registration no longer states its paths where this can see them", len(served))
	}
}

// servedRoutes reads the paths this package registers, as segment lists with
// {param} standing for any one segment.
func servedRoutes(t *testing.T) [][]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	route := regexp.MustCompile(`(?:mux|hostMux)\.HandleFunc\("(?:\w+ )?(/[^"]*)"`)
	var out [][]string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range route.FindAllStringSubmatch(string(src), -1) {
			out = append(out, segments(m[1]))
		}
	}
	return out
}

func segments(p string) []string {
	var out []string
	for s := range strings.SplitSeq(strings.Trim(p, "/"), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// servedBy reports whether a route answers the path. A call that builds its
// last segment ("/plugins/" + name) states the prefix and nothing else, so a
// trailing slash matches a route that takes one more segment.
func servedBy(routes [][]string, path string) bool {
	want := segments(path)
	open := strings.HasSuffix(path, "/")
	for _, r := range routes {
		if len(r) < len(want) || (len(r) != len(want) && !open) {
			continue
		}
		match := true
		for i, seg := range want {
			if !strings.HasPrefix(r[i], "{") && r[i] != seg {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
