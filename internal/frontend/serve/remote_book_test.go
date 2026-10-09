package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func bookServer(t *testing.T, attacher RemoteAttacher) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	front := httptest.NewServer(operatorHandler(NewHub(HubOptions{Remote: attacher})))
	t.Cleanup(front.Close)
	return front
}

func bookPost(t *testing.T, front *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(front.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// Turning a machine off says nothing about what this window shows, so the row
// keeps reporting it and the rail keeps listing it.
func TestADisabledHostIsStillListed(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4","disabled":true}`)

	resp, err := http.Get(front.URL + "/remotes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed []struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != "gpu-box" || !listed[0].Disabled {
		t.Fatalf("listed = %+v, want the disabled machine reported", listed)
	}
}

// A machine the book has turned off is not dialed at all: the refusal is typed,
// and the link layer never hears about it.
func TestOpeningADisabledHostIsRefusedWithoutDialing(t *testing.T) {
	dialed := false
	front := bookServer(t, &stubAttacher{attach: func(host, workspace string) (RemoteEndpoint, func(), error) {
		dialed = true
		return RemoteEndpoint{Host: host, Workspace: workspace}, func() {}, nil
	}})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4","disabled":true}`)

	resp := bookPost(t, front, "/remotes/open", `{"host":"gpu-box"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("opening a disabled machine = %d, want 409", resp.StatusCode)
	}
	if dialed {
		t.Fatal("a disabled machine was dialed")
	}
}

// A question about a disabled machine is refused too, because answering it would
// take the same link the switch exists to avoid.
func TestAskingAboutADisabledMachineIsRefused(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4","disabled":true}`)

	resp, err := http.Get(front.URL + "/remotes/gpu-box/tree")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("tree read of a disabled machine = %d, want 409", resp.StatusCode)
	}
	var refused map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&refused); err != nil {
		t.Fatal(err)
	}
	if refused["code"] != "remote.disabled" {
		t.Fatalf("refusal = %v, want code remote.disabled", refused)
	}
}

// The flag is the book's, so it round-trips and the fields the page cannot see
// come back with it; turning it off again is an ordinary save.
func TestDisabledSurvivesASaveAndKeepsTheRest(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	bookPost(t, front, "/remotes",
		`{"name":"gpu-box","host":"10.0.0.4","workspaces":["/srv/training"],"disabled":true}`)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.RemoteHost("gpu-box")
	if !ok {
		t.Fatal("the host never reached the book")
	}
	if !entry.Disabled {
		t.Fatalf("entry = %+v, want disabled", entry)
	}
	if got := entry.WorkspaceList(); len(got) != 1 || got[0] != "/srv/training" {
		t.Fatalf("workspaces = %v, want the one the row carried", got)
	}

	resp := bookPost(t, front, "/remotes",
		`{"name":"gpu-box","host":"10.0.0.4","workspaces":["/srv/training"],"disabled":false}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("turning it back on = %d, want 204", resp.StatusCode)
	}
}

// A machine is reachable from every project on this computer, so its entry
// belongs to the user and not to whichever folder was open when it was added.
func TestSavingAHostWritesTheUserBook(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	resp := bookPost(t, front, "/remotes",
		`{"name":"gpu-box","host":"10.0.0.4","user":"ada","port":2222,"workspace":"/srv/training"}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.RemoteHost("gpu-box")
	if !ok {
		t.Fatal("the host never reached the book")
	}
	if entry.Host != "10.0.0.4" || entry.User != "ada" || entry.Port != 2222 {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Workspace != "/srv/training" {
		t.Fatalf("workspace = %q", entry.Workspace)
	}
}

// An entry layering ssh_config has its address written next door; one that does
// not has nowhere else to get it, and a row with neither dials nothing.
func TestAHostNeedsAnAddressUnlessSSHConfigHasIt(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	resp := bookPost(t, front, "/remotes", `{"name":"gpu-box"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "remote.host_required" {
		t.Fatalf("code = %q", body.Code)
	}

	// The same row with ssh_config layered under it is complete: the alias is
	// the address, which is the whole point of importing one.
	if got := bookPost(t, front, "/remotes", `{"name":"gpu-box","useSSHConfig":true}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("ssh_config entry rejected: %d", got)
	}
	cfg, _ := config.Load()
	entry, _ := cfg.RemoteHost("gpu-box")
	if entry.Host != "gpu-box" || !entry.UseSSHConfig {
		t.Fatalf("entry = %+v, want the alias standing in for the address", entry)
	}
}

// An idle pane on the machine is no reason to keep its row: the pane closes
// with it, so no link outlives the entry that accounts for it.
func TestRemovingAHostClosesItsIdlePanes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	rk := fakeRemoteKernel(t)
	h := NewHub(HubOptions{Remote: &stubAttacher{}})
	if _, err := h.OpenRemote(RemoteEndpoint{
		Host: "gpu-box", Workspace: "/srv/training", Addr: rk.Listener.Addr().String(), Token: "t",
	}, nil); err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(operatorHandler(h))
	defer front.Close()

	resp := bookPost(t, front, "/remotes/remove", `{"name":"gpu-box"}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if n := len(h.Runtimes()); n != 0 {
		t.Fatalf("%d panes left on a removed machine, want none", n)
	}
}

// Work in progress is the one thing a removal waits for, and waiting closes
// nothing: the running pane and its idle sibling both stay.
func TestRemovingAHostIsRefusedWhileItsPaneRuns(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"running": r.URL.Path == "/rt/busy/status"})
	}))
	defer far.Close()
	h := NewHub(HubOptions{Remote: &stubAttacher{}})
	for _, base := range []string{"/rt/busy", "/rt/idle"} {
		if _, err := h.OpenRemote(RemoteEndpoint{
			Host: "gpu-box", Workspace: "/srv/training", Addr: far.Listener.Addr().String(), Token: "t", Base: base,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	front := httptest.NewServer(operatorHandler(h))
	defer front.Close()

	resp := bookPost(t, front, "/remotes/remove", `{"name":"gpu-box"}`)
	var body struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusConflict || body.Code != "remote.running" {
		t.Fatalf("remove = %d/%q, want 409/remote.running", resp.StatusCode, body.Code)
	}
	if n, _ := body.Params["n"].(float64); n != 1 {
		t.Fatalf("params.n = %v, want the one running pane", body.Params["n"])
	}
	if n := len(h.Runtimes()); n != 2 {
		t.Fatalf("%d panes left, want both kept while one runs", n)
	}
}

// Offering an alias the book already holds would invite a duplicate row for one
// machine, each with its own connection.
func TestCandidatesLeaveOutWhatTheBookAlreadyHas(t *testing.T) {
	front := bookServer(t, &stubAttacher{candidates: []string{"gpu-box", "builder", "attic"}})
	if got := bookPost(t, front, "/remotes", `{"name":"builder","host":"build.internal"}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("save failed: %d", got)
	}
	resp, err := http.Get(front.URL + "/remotes/candidates")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0] != "gpu-box" || out[1] != "attic" {
		t.Fatalf("candidates = %v, want the two not in the book", out)
	}
}

// The page has no control for forwards — they are set from the CLI — so an edit
// made here must not be how they disappear.
func TestEditingAHostKeepsForwardsThePageCannotSee(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	if got := bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("save failed: %d", got)
	}
	if err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		entry, _ := c.RemoteHost("gpu-box")
		entry.Forwards = []config.RemoteForwardEntry{{Type: "local", Bind: "127.0.0.1:8080", Target: "127.0.0.1:80"}}
		return nil, c.UpsertRemoteHost(entry)
	}); err != nil {
		t.Fatal(err)
	}

	if got := bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.9"}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("edit failed: %d", got)
	}
	cfg, _ := config.Load()
	entry, _ := cfg.RemoteHost("gpu-box")
	if entry.Host != "10.0.0.9" {
		t.Fatalf("the edit did not land: %+v", entry)
	}
	if len(entry.Forwards) != 1 || entry.Forwards[0].Bind != "127.0.0.1:8080" {
		t.Fatalf("the edit dropped the forwards: %+v", entry.Forwards)
	}
}

// The far machine's own workspace list. With no pane open the read takes a link
// for itself and gives it back, so the book of a machine that is not connected
// can still be listed, and listing it opens nothing.
func TestRemoteTreeIsReadWithOrWithoutAnOpenPane(t *testing.T) {
	writeOpenableConfig(t)
	far := NewHub(HubOptions{})
	defer far.Shutdown()
	farSide := httptest.NewServer(operatorHandler(far))
	defer farSide.Close()

	var attached, released int
	near := NewHub(HubOptions{Remote: &stubAttacher{
		attach: func(host, workspace string) (RemoteEndpoint, func(), error) {
			attached++
			return RemoteEndpoint{
				Host: host, Workspace: workspace,
				Addr: farSide.Listener.Addr().String(), Token: "t",
			}, func() { released++ }, nil
		},
	}})
	nearSide := httptest.NewServer(operatorHandler(near))
	defer nearSide.Close()

	panes := len(near.Runtimes())
	resp, err := http.Get(nearSide.URL + "/remotes/gpu-box/tree")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unconnected host tree = %d, want 200", resp.StatusCode)
	}
	if attached != 1 || released != 1 {
		t.Fatalf("link taken %d and given back %d times, want once each", attached, released)
	}
	if got := len(near.Runtimes()); got != panes {
		t.Fatalf("reading the book published a pane: %d runtimes, want %d", got, panes)
	}

	workspace := testenv.TempDir(t)
	open, err := http.Post(nearSide.URL+"/remotes/open", "application/json", openRemoteBody(t, "gpu-box", workspace))
	if err != nil {
		t.Fatal(err)
	}
	defer open.Body.Close()
	if open.StatusCode != http.StatusOK {
		t.Fatalf("open remote = %d", open.StatusCode)
	}

	resp, err = http.Get(nearSide.URL + "/remotes/gpu-box/tree")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tree = %d, want 200", resp.StatusCode)
	}
	var tree []struct {
		Root string `json:"root"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ws := range tree {
		if ws.Root == workspace {
			found = true
		}
	}
	if !found {
		t.Fatalf("the far machine's tree does not list the workspace a pane opened: %+v", tree)
	}
}

// The complaint this answers: one machine, several projects, and a book that
// could only name one of them. The list goes out default-first so the sidebar
// can draw every folder before a link exists to ask the far kernel through.
func TestAHostCarriesEveryProjectOnIt(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	resp := bookPost(t, front, "/remotes",
		`{"name":"gpu-box","host":"10.0.0.4","workspaces":["/srv/train","/srv/eval","/srv/train"]}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	listed, err := http.Get(front.URL + "/remotes")
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	var out []RemoteHostView
	if err := json.NewDecoder(listed.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("hosts = %+v", out)
	}
	if got := out[0].Workspaces; len(got) != 2 || got[0] != "/srv/train" || got[1] != "/srv/eval" {
		t.Fatalf("workspaces = %v, want both folders once each, default first", got)
	}
	// The single field keeps meaning what it always did, so a CLI connect and
	// an older window still land in the same place.
	if out[0].Workspace != "/srv/train" {
		t.Fatalf("default workspace = %q, want the head of the list", out[0].Workspace)
	}
}

// A row saved by a window that predates the list still names its one folder,
// and that folder is the whole list rather than a case every reader repeats.
func TestAHostSavedWithOnlyTheOldFieldIsAListOfOne(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	if resp := bookPost(t, front, "/remotes",
		`{"name":"gpu-box","host":"10.0.0.4","workspace":"/srv/training"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	listed, err := http.Get(front.URL + "/remotes")
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	var out []RemoteHostView
	if err := json.NewDecoder(listed.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Workspaces) != 1 || out[0].Workspaces[0] != "/srv/training" {
		t.Fatalf("workspaces = %+v", out)
	}
}

// The sidebar writes one field of a row. Sending the whole row from a control
// that only knows a folder is what would blank the address beside it.
func TestAddingAFolderKeepsTheRestOfTheRow(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	if got := bookPost(t, front, "/remotes",
		`{"name":"gpu-box","host":"10.0.0.4","user":"ada","workspaces":["/srv/training"]}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("save = %d", got)
	}
	if got := bookPost(t, front, "/remotes/gpu-box/workspaces", `{"path":"/srv/eval"}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("add folder = %d, want 204", got)
	}
	entry := bookEntry(t, "gpu-box")
	if entry.Host != "10.0.0.4" || entry.User != "ada" {
		t.Fatalf("the row lost fields the sidebar never saw: %+v", entry)
	}
	// Head is the default, and adding is not promoting: a folder picked once
	// must not take over where a bare connect lands.
	if got := entry.WorkspaceList(); len(got) != 2 || got[0] != "/srv/training" || got[1] != "/srv/eval" {
		t.Fatalf("folders = %v, want the new one after the default", got)
	}
}

func TestAddingAFolderTwiceLeavesOneRow(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`)
	bookPost(t, front, "/remotes/gpu-box/workspaces", `{"path":"/srv/training"}`)
	bookPost(t, front, "/remotes/gpu-box/workspaces", `{"path":"/srv/training"}`)
	if got := bookEntry(t, "gpu-box").WorkspaceList(); len(got) != 1 {
		t.Fatalf("folders = %v, want one", got)
	}
}

// Dropping the default promotes the next: a list with no head is a machine
// that forgot where a bare connect lands.
func TestRemovingTheDefaultFolderPromotesTheNext(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4","workspaces":["/srv/training","/srv/eval"]}`)
	if got := bookPost(t, front, "/remotes/gpu-box/workspaces/remove", `{"path":"/srv/training"}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", got)
	}
	entry := bookEntry(t, "gpu-box")
	if entry.Workspace != "/srv/eval" || len(entry.Workspaces) != 0 {
		t.Fatalf("entry = %+v, want /srv/eval as the only folder and the default", entry)
	}
}

func TestWritingAFolderOntoAMachineTheBookDoesNotHaveIsRefused(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	if got := bookPost(t, front, "/remotes/absent/workspaces", `{"path":"/srv/x"}`).StatusCode; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

func TestAFolderWriteNeedsAPath(t *testing.T) {
	front := bookServer(t, &stubAttacher{})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`)
	if got := bookPost(t, front, "/remotes/gpu-box/workspaces", `{"path":"  "}`).StatusCode; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

// A folder opened once is a folder the sidebar can offer next launch. Nothing
// else survives a cold start: the far machine's own list needs a kernel over
// there, and there is none until something is open.
func TestOpeningARemoteFolderWritesItIntoTheBook(t *testing.T) {
	writeOpenableConfig(t)
	far := NewHub(HubOptions{})
	defer far.Shutdown()
	farSide := httptest.NewServer(operatorHandler(far))
	defer farSide.Close()

	workspace := testenv.TempDir(t)
	near := NewHub(HubOptions{Remote: &stubAttacher{
		attach: func(host, _ string) (RemoteEndpoint, func(), error) {
			// What the far side resolved, which is not always what was asked
			// for: ~ is expanded over there.
			return RemoteEndpoint{
				Host: host, Workspace: workspace,
				Addr: farSide.Listener.Addr().String(), Token: "t",
			}, func() {}, nil
		},
	}})
	defer near.Shutdown()
	nearSide := httptest.NewServer(operatorHandler(near))
	defer nearSide.Close()
	if got := bookPost(t, nearSide, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`).StatusCode; got != http.StatusNoContent {
		t.Fatalf("save host = %d", got)
	}

	open, err := http.Post(nearSide.URL+"/remotes/open", "application/json", openRemoteBody(t, "gpu-box", "~/scratch"))
	if err != nil {
		t.Fatal(err)
	}
	defer open.Body.Close()
	if open.StatusCode != http.StatusOK {
		t.Fatalf("open remote = %d", open.StatusCode)
	}
	if got := bookEntry(t, "gpu-box").WorkspaceList(); len(got) != 1 || got[0] != workspace {
		t.Fatalf("folders = %v, want the path the far side answered with (%q)", got, workspace)
	}
}

func bookEntry(t *testing.T, name string) config.RemoteHostEntry {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.RemoteHost(name)
	if !ok {
		t.Fatalf("no %q in the book", name)
	}
	return entry
}

// A folder with an idle pane on it is dropped when asked, and the pane goes
// with it; only a pane mid-turn would hold the row.
func TestAFolderWithAnIdlePaneOnItIsDroppedWithThePane(t *testing.T) {
	writeOpenableConfig(t)
	far := NewHub(HubOptions{})
	defer far.Shutdown()
	farSide := httptest.NewServer(operatorHandler(far))
	defer farSide.Close()

	workspace := testenv.TempDir(t)
	near := NewHub(HubOptions{Remote: &stubAttacher{
		attach: func(host, _ string) (RemoteEndpoint, func(), error) {
			return RemoteEndpoint{
				Host: host, Workspace: workspace,
				Addr: farSide.Listener.Addr().String(), Token: "t",
			}, func() {}, nil
		},
	}})
	defer near.Shutdown()
	nearSide := httptest.NewServer(operatorHandler(near))
	defer nearSide.Close()
	bookPost(t, nearSide, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`)

	open, err := http.Post(nearSide.URL+"/remotes/open", "application/json", openRemoteBody(t, "gpu-box", workspace))
	if err != nil {
		t.Fatal(err)
	}
	open.Body.Close()

	drop, err := json.Marshal(map[string]string{"path": workspace})
	if err != nil {
		t.Fatal(err)
	}
	resp := bookPost(t, nearSide, "/remotes/gpu-box/workspaces/remove", string(drop))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204: an idle pane closes with its folder", resp.StatusCode)
	}
	if got := bookEntry(t, "gpu-box").WorkspaceList(); len(got) != 0 {
		t.Fatalf("the folder is still in the book: %v", got)
	}
	if n := len(near.Runtimes()); n != 0 {
		t.Fatalf("%d panes left in a removed folder, want none", n)
	}
}

// Every pane on a machine passes through the remembering step, and nearly all
// of them land on a folder the book already has. Rewriting the user's config
// for each one is a file touched for nothing — and one that Studio holding it
// open makes other readers' problem.
func TestReopeningAKnownFolderLeavesTheConfigAlone(t *testing.T) {
	writeOpenableConfig(t)
	far := NewHub(HubOptions{})
	defer far.Shutdown()
	farSide := httptest.NewServer(operatorHandler(far))
	defer farSide.Close()

	workspace := testenv.TempDir(t)
	near := NewHub(HubOptions{Remote: &stubAttacher{
		attach: func(host, _ string) (RemoteEndpoint, func(), error) {
			return RemoteEndpoint{
				Host: host, Workspace: workspace,
				Addr: farSide.Listener.Addr().String(), Token: "t",
			}, func() {}, nil
		},
	}})
	defer near.Shutdown()
	nearSide := httptest.NewServer(operatorHandler(near))
	defer nearSide.Close()
	bookPost(t, nearSide, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`)

	open := func() {
		resp, err := http.Post(nearSide.URL+"/remotes/open", "application/json", openRemoteBody(t, "gpu-box", workspace))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	open()

	// Stamped rather than slept on: a second write is what this is about, not
	// how long one takes.
	path := config.UserConfigPath()
	then := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatal(err)
	}
	open()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(then) {
		t.Fatal("opening a folder the book already holds rewrote the config file")
	}
}

// Deleting a conversation on another machine goes to that machine's kernel over
// a link taken for the call, and a refusal comes back under that kernel's own
// code: it knows the conversation is open there, the window does not.
func TestRemoteSessionIsRemovedByTheFarKernel(t *testing.T) {
	var got []string
	refuseWith := ""
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, r.Method+" "+r.URL.Path+" "+body.Path)
		if refuseWith != "" {
			refuse(w, http.StatusConflict, refuseWith, "close this session's pane first", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer far.Close()
	var attached, released int
	near := NewHub(HubOptions{Remote: &stubAttacher{
		attach: func(host, workspace string) (RemoteEndpoint, func(), error) {
			attached++
			return RemoteEndpoint{Host: host, Addr: far.Listener.Addr().String(), Token: "t"}, func() { released++ }, nil
		},
	}})
	defer near.Shutdown()
	nearSide := httptest.NewServer(operatorHandler(near))
	defer nearSide.Close()

	remove := func() *http.Response {
		resp, err := http.Post(nearSide.URL+"/remotes/gpu-box/sessions/remove", "application/json",
			strings.NewReader(`{"path":"/home/t/proj/.reasonix/sessions/a.jsonl"}`))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := remove()
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", resp.StatusCode)
	}
	if len(got) != 1 || got[0] != "POST /tree/sessions/remove /home/t/proj/.reasonix/sessions/a.jsonl" {
		t.Fatalf("far kernel saw %v", got)
	}
	if attached != 1 || released != 1 {
		t.Fatalf("link taken %d and given back %d times, want once each", attached, released)
	}

	refuseWith = "session.has_open_pane"
	resp = remove()
	defer resp.Body.Close()
	var reason Reason
	if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict || reason.Code != "session.has_open_pane" {
		t.Fatalf("refusal = %d %q, want the far kernel's own 409 session.has_open_pane", resp.StatusCode, reason.Code)
	}
}
