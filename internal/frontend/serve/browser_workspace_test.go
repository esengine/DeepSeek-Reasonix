package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
)

// A browser on a headless server cannot open a native picker, but the path API
// is the whole workspace mutation and must remain usable.
func TestHeadlessBrowserAddsWorkspaceByPath(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	srv := httptest.NewServer(operatorHandler(NewHub(HubOptions{})))
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]string{"path": root})
	if err != nil {
		t.Fatal(err)
	}
	status, code := postLaunchJSON(t, srv.URL+"/tree/workspaces", string(body), nil)
	if status != http.StatusOK {
		t.Fatalf("POST /tree/workspaces = %d %s", status, code)
	}

	tree := hubGet[[]treeWorkspace](t, srv, "/tree")
	for _, ws := range tree {
		if ws.Root == root {
			if !ws.Remembered {
				t.Fatal("added workspace was visible but not remembered")
			}
			return
		}
	}
	t.Fatalf("/tree does not contain added workspace %q: %+v", root, tree)
}
