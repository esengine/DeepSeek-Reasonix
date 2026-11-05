package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// nullPaths lists where a decoded JSON document holds a null. A Go nil slice
// encodes to null, and the window reads those lists with .filter or .map: a
// list that is empty has to be sent as [].
func nullPaths(v any, at string, out *[]string) {
	switch x := v.(type) {
	case nil:
		*out = append(*out, at)
	case map[string]any:
		for k, child := range x {
			nullPaths(child, at+"."+k, out)
		}
	case []any:
		for i, child := range x {
			nullPaths(child, fmt.Sprintf("%s[%d]", at, i), out)
		}
	}
}

// The reads a window makes when it opens a session and while it sits there.
// Their lists are read unguarded on the other side, so none may be null.
func TestSessionReadsNeverSendANullList(t *testing.T) {
	s := noProviderServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	hub := NewHub(HubOptions{})
	hubSrv := httptest.NewServer(hub.Handler())
	defer hubSrv.Close()

	var found []string
	reads := map[string][]string{hubSrv.URL: {"/tree", "/runtimes", "/remotes", "/share", "/share/remember", "/host/capabilities", "/studio/versions", "/asks"}}
	reads[srv.URL] = append(reads[srv.URL], []string{
		"/history", "/status", "/trajectory", "/checkpoints", "/todos", "/changes", "/context", "/inbox",
		"/mcp", "/extensions/surfaces", "/extensions/actions", "/surfaces", "/workspace/git", "/models",
		"/rewind/undo", "/branches", "/execution-graph", "/adjudications", "/sessions", "/skills", "/hooks",
		"/providers", "/browser/tabs", "/memory", "/workspaces", "/fold-work-settings", "/settings-changed",
		"/changed-files-settings", "/events/replay?lastEventId=0",
	}...)
	for base, paths := range reads {
		for _, path := range paths {
			found = append(found, nullsOf(t, base, path)...)
		}
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("null in a read: %s", f)
	}
}

func nullsOf(t *testing.T, base, path string) []string {
	t.Helper()
	{
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil
		}
		var doc any
		if json.Unmarshal(raw, &doc) != nil {
			return nil
		}
		var nulls, found []string
		nullPaths(doc, "", &nulls)
		for _, n := range nulls {
			if path == "/rewind/undo" && n == "" {
				continue // no undo on offer is the whole answer, and the window reads it as null
			}
			found = append(found, path+" "+n)
		}
		return found
	}
}
