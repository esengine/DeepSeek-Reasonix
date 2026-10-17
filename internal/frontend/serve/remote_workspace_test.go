package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestRemoteWorkspaceResolutionForReadAndOpen(t *testing.T) {
	for _, tc := range []struct {
		name, settings, want string
	}{
		{"declared default", `workspace = "/srv/default"
workspaces = ["/srv/other", "/srv/default"]`, "/srv/default"},
		{"reordered extras", `workspace = "/srv/default"
workspaces = ["/srv/third", "/srv/other"]`, "/srv/default"},
		{"no declared default", `workspaces = ["/srv/first", "/srv/second"]`, "/srv/first"},
		{"no workspace", "", ""},
	} {
		for _, operation := range []string{"tree", "open", "explicit open"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				want := tc.want
				if operation == "explicit open" {
					want = "/srv/explicit"
				}
				far := remoteWorkspaceKernel(t)
				var attached []string
				front := bookServer(t, &stubAttacher{attach: func(host, dir string) (RemoteEndpoint, func(), error) {
					attached = append(attached, dir)
					if dir == "" {
						dir = "/srv/kernel"
					}
					return RemoteEndpoint{Host: host, Workspace: dir, Addr: far.Listener.Addr().String(), Token: "test"}, func() {}, nil
				}})
				writeRemoteWorkspaceConfig(t, tc.settings)
				resp := remoteWorkspaceRequest(t, front, operation)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK || len(attached) != 1 || attached[0] != want {
					t.Fatalf("status=%d attached=%q, want 200 and [%q]", resp.StatusCode, attached, want)
				}
			})
		}
	}
}

func TestRemoteWorkspaceResolutionIgnoresOtherOpenPanesAndRefusesDisabledHosts(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		for _, operation := range []string{"tree", "open"} {
			t.Run(fmt.Sprintf("disabled=%t/%s", disabled, operation), func(t *testing.T) {
				far := remoteWorkspaceKernel(t)
				var attached []string
				front := bookServer(t, &stubAttacher{attach: func(host, dir string) (RemoteEndpoint, func(), error) {
					attached = append(attached, dir)
					return RemoteEndpoint{Host: host, Workspace: dir, Addr: far.Listener.Addr().String(), Token: "test"}, func() {}, nil
				}})
				writeRemoteWorkspaceConfig(t, `workspace = "/srv/default"`)
				opened := bookPost(t, front, "/remotes/open", `{"host":"gpu-box","workspace":"/srv/other"}`)
				if opened.StatusCode != http.StatusOK {
					t.Fatalf("explicit open = %d", opened.StatusCode)
				}
				if disabled {
					resp := bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"example.invalid","workspace":"/srv/default","disabled":true}`)
					if resp.StatusCode != http.StatusNoContent {
						t.Fatalf("disable = %d", resp.StatusCode)
					}
				}
				attached = nil
				resp := remoteWorkspaceRequest(t, front, operation)
				defer resp.Body.Close()
				if disabled {
					assertRemoteWorkspaceDisabled(t, resp)
					if len(attached) != 0 {
						t.Fatalf("disabled host dialed: %q", attached)
					}
				} else if resp.StatusCode != http.StatusOK || len(attached) != 1 || attached[0] != "/srv/default" {
					t.Fatalf("status=%d attached=%q, want configured default", resp.StatusCode, attached)
				}
			})
		}
	}
}

func writeRemoteWorkspaceConfig(t *testing.T, settings string) {
	t.Helper()
	body := "[[remote.hosts]]\nname = \"gpu-box\"\nhost = \"example.invalid\"\n" + settings + "\n"
	if err := os.WriteFile(config.UserConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`[[remote.hosts]]
name = "gpu-box"
host = "untrusted.invalid"
workspace = "/repo/injected"
workspaces = ["/repo/injected"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
}

func remoteWorkspaceKernel(t *testing.T) *httptest.Server {
	t.Helper()
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtimes":
			writeJSON(w, RuntimeView{ID: "far", Base: "/rt/far", Root: "/srv/kernel", Name: "project"})
		case "/tree":
			writeJSON(w, []treeWorkspace{})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(far.Close)
	return far
}

func remoteWorkspaceRequest(t *testing.T, front *httptest.Server, operation string) *http.Response {
	t.Helper()
	if operation == "tree" {
		resp, err := http.Get(front.URL + "/remotes/gpu-box/tree")
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	body := `{"host":"gpu-box"}`
	if operation == "explicit open" {
		body = `{"host":"gpu-box","workspace":"/srv/explicit"}`
	}
	return bookPost(t, front, "/remotes/open", body)
}

func assertRemoteWorkspaceDisabled(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 remote.disabled", resp.StatusCode)
	}
	var reason Reason
	if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict || reason.Code != "remote.disabled" {
		t.Fatalf("status=%d reason=%+v, want remote.disabled", resp.StatusCode, reason)
	}
}
