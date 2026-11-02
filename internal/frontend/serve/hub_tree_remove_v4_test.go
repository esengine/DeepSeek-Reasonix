package serve

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/sessionv4/v4fixture"
)

func TestRemoveV4SessionStaysRemoved(t *testing.T) {
	for _, imported := range []bool{false, true} {
		name := "unopened"
		if imported {
			name = "imported"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", t.TempDir())
			t.Setenv("REASONIX_STATE_HOME", t.TempDir())
			root := t.TempDir()
			rememberWorkspace(root)
			dir := SessionDirFor(root)
			f := v4fixture.New(t)
			f.Root = filepath.Join(filepath.Dir(dir), "sessions-v4")
			source := f.Session("desktop-000000000000000000000001", 3)
			f.Batch(source, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("a", "user", "legacy conversation")})
			sourceLog := filepath.Join(source, "events.frames")
			before, err := os.ReadFile(sourceLog)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "v4-desktop-000000000000000000000001.jsonl")
			if imported {
				if _, err := sessionstore.LoadSession(path); err != nil {
					t.Fatal(err)
				}
			}
			kept := filepath.Join(dir, "keep.jsonl")
			writeSessionAt(t, kept)
			h := NewHub(HubOptions{})
			t.Cleanup(h.Shutdown)
			srv := httptest.NewServer(operatorHandler(h))
			defer srv.Close()
			deadline := time.Now().Add(5 * time.Second)
			for {
				tree := hubGet[[]treeWorkspace](t, srv, "/tree?progressive=1")
				if len(tree) == 1 && tree[0].CountsComplete != nil && *tree[0].CountsComplete && len(tree[0].Sessions) == 2 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("listing did not settle: %+v", tree)
				}
				time.Sleep(10 * time.Millisecond)
			}
			resp := postRemoveSession(t, srv, path)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("remove status = %d", resp.StatusCode)
			}
			check := func(server *httptest.Server) {
				t.Helper()
				for _, route := range []string{"/tree", "/tree?progressive=1"} {
					tree := hubGet[[]treeWorkspace](t, server, route)
					if len(tree) != 1 || len(tree[0].Sessions) != 1 || tree[0].Sessions[0].Path != kept {
						t.Errorf("%s after removal = %+v, want only %s", route, tree, kept)
					}
				}
			}
			check(srv)
			h.Shutdown()
			restarted := NewHub(HubOptions{})
			t.Cleanup(restarted.Shutdown)
			again := httptest.NewServer(operatorHandler(restarted))
			defer again.Close()
			check(again)
			if _, err := sessionstore.LoadSession(path); !os.IsNotExist(err) {
				t.Errorf("removed legacy session reopened: %v", err)
			}
			after, err := os.ReadFile(sourceLog)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("read-only legacy source changed: %v", err)
			}
		})
	}
}
