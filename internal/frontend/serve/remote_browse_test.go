package serve

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func browseGet(t *testing.T, front *httptest.Server, path string) (*http.Response, RemoteListing, Reason) {
	t.Helper()
	resp, err := http.Get(front.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode == http.StatusOK {
		var out RemoteListing
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp, out, Reason{}
	}
	var why Reason
	_ = json.NewDecoder(resp.Body).Decode(&why)
	return resp, RemoteListing{}, why
}

// The whole reason browsing is not a question for a pane: nothing is open on
// this machine and it still answers, because the link layer reads the folder
// over the file protocol rather than through a kernel it would have to install.
func TestBrowsingAnswersWithNoPaneOnTheMachine(t *testing.T) {
	front := bookServer(t, &stubAttacher{browse: func(host, dir string) (RemoteListing, error) {
		if host != "gpu-box" || dir != "" {
			t.Fatalf("browse(%q, %q), want the host and its login home", host, dir)
		}
		return RemoteListing{
			Path:   "/home/ada",
			Parent: "/home",
			Folders: []RemoteFolder{
				{Name: "eval", Path: "/home/ada/eval"},
				{Name: "training", Path: "/home/ada/training"},
			},
		}, nil
	}})
	resp, got, _ := browseGet(t, front, "/remotes/gpu-box/dirs")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got.Path != "/home/ada" || got.Parent != "/home" || len(got.Folders) != 2 {
		t.Fatalf("listing = %+v", got)
	}
	if got.Folders[1].Path != "/home/ada/training" {
		t.Fatalf("folder = %+v, want the far machine's own path", got.Folders[1])
	}
}

// A path the far machine spells with a drive letter reaches it unchanged: this
// side has no business normalising a syntax that is not its own.
func TestTheAskedForPathReachesTheFarMachineAsWritten(t *testing.T) {
	var seen string
	front := bookServer(t, &stubAttacher{browse: func(_, dir string) (RemoteListing, error) {
		seen = dir
		return RemoteListing{Path: dir}, nil
	}})
	want := `/C:/Users/ada/pipe line`
	browseGet(t, front, "/remotes/box/dirs?path="+url.QueryEscape(want))
	if seen != want {
		t.Fatalf("the link layer was asked for %q, want %q", seen, want)
	}
}

// An empty folder is an empty list, never null: a picker that has to tell the
// two apart is one JSON decoder away from rendering nothing at all.
func TestAFolderWithNoSubfoldersAnswersAnEmptyList(t *testing.T) {
	front := bookServer(t, &stubAttacher{browse: func(string, string) (RemoteListing, error) {
		return RemoteListing{Path: "/srv/empty"}, nil
	}})
	resp, err := http.Get(front.URL + "/remotes/box/dirs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["folders"]) != "[]" {
		t.Fatalf("folders = %s, want []", raw["folders"])
	}
}

// A mistyped path is fixed in the picker; a link that is down is fixed
// somewhere else entirely. Only the side that read the file protocol's answer
// knows which happened, so its code has to survive the trip out.
func TestABrowseRefusalKeepsTheCodeTheLinkLayerGaveIt(t *testing.T) {
	front := bookServer(t, &stubAttacher{browse: func(_, dir string) (RemoteListing, error) {
		return RemoteListing{}, Refusal(http.StatusNotFound, "remote.no_such_folder",
			errors.New("no such file"), map[string]any{"path": dir})
	}})
	resp, _, why := browseGet(t, front, "/remotes/box/dirs?path=/srv/typo")
	if resp.StatusCode != http.StatusNotFound || why.Code != "remote.no_such_folder" {
		t.Fatalf("refusal = %d/%q, want 404/remote.no_such_folder", resp.StatusCode, why.Code)
	}
	if why.Params["path"] != "/srv/typo" {
		t.Fatalf("params = %v, want the path that was not there", why.Params)
	}
}

// A kernel serving a browser tab dials nowhere on a request's say-so, and the
// picker has to be able to tell that apart from a machine that refused.
func TestBrowsingIsNotOfferedWhereRemotePanesAreNot(t *testing.T) {
	front := bookServer(t, nil)
	resp, _, why := browseGet(t, front, "/remotes/box/dirs")
	if resp.StatusCode != http.StatusNotImplemented || why.Code != "remote.not_available" {
		t.Fatalf("no-remote kernel = %d/%q, want 501/remote.not_available", resp.StatusCode, why.Code)
	}
}

// A probe answers without a pane for the same reason browsing does: it rides
// the connection alone. Its value is that one call reports every closed route,
// where a connect would have failed on the first and said nothing about the
// rest.
func TestProbeReportsEveryRouteInOneAnswer(t *testing.T) {
	front := bookServer(t, &stubAttacher{probe: &RemoteProbe{
		OS: "linux", Arch: "amd64", Home: "/home/ada", Ready: false,
		Routes: []RemoteProbeRoute{
			{Name: "npm", Code: "remote.npm_unavailable"},
			{Name: "upload", Code: "remote.platform_mismatch"},
			{Name: "download", OK: true},
		},
	}})
	resp, err := http.Get(front.URL + "/remotes/gpu-box/probe")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out RemoteProbe
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Routes) != 3 || out.Ready {
		t.Fatalf("probe = %+v", out)
	}
	// A closed route carries the code, not a sentence: the window words it the
	// same way it words the failure itself.
	if out.Routes[0].Code != "remote.npm_unavailable" || out.Routes[2].Code != "" {
		t.Errorf("codes = %+v", out.Routes)
	}
}

// A machine with every route open still has to send an array. A nil slice
// encodes as null, and a frontend mapping over it throws where it should have
// drawn nothing.
func TestProbeSendsRoutesAsAnArray(t *testing.T) {
	front := bookServer(t, &stubAttacher{probe: &RemoteProbe{OS: "linux", Arch: "amd64", Ready: true}})
	resp, err := http.Get(front.URL + "/remotes/gpu-box/probe")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"routes":[]`) {
		t.Fatalf("routes did not encode as an array: %s", body)
	}
}

// Without a link layer the window is a browser tab talking to a bare serve,
// and a probe there would be this process SSH-ing somewhere on its own say-so.
func TestProbeRefusesWithoutALinkLayer(t *testing.T) {
	front := bookServer(t, nil)
	resp, err := http.Get(front.URL + "/remotes/gpu-box/probe")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a hub with no attacher answered a probe")
	}
	var why Reason
	_ = json.NewDecoder(resp.Body).Decode(&why)
	if why.Code == "" {
		t.Fatal("the refusal carried no code")
	}
}
