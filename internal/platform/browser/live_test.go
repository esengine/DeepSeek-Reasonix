package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// liveSession starts a real browser when REASONIX_LIVE_BROWSER names one (or
// is "1" to discover an installed one). Normal runs skip: CI machines differ in
// whether any Chromium is installed.
func liveSession(t *testing.T, headless bool) *Session {
	t.Helper()
	want := os.Getenv("REASONIX_LIVE_BROWSER")
	if want == "" {
		t.Skip("set REASONIX_LIVE_BROWSER=1 or to a browser executable to run live browser tests")
	}
	configured := want
	if want == "1" {
		configured = ""
	}
	exe, err := Discover(configured)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	root := testenv.TempDir(t)
	s := NewSession(Config{
		Launch: LaunchSpec{Executable: exe, ProfileDir: testenv.TempDir(t), Headless: headless},
		Roots:  []string{root},
	})
	t.Cleanup(s.Close)
	return s
}

func liveServer(t *testing.T, pages map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range pages {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLiveOpenLoadsAPage(t *testing.T) {
	s := liveSession(t, true)
	srv := liveServer(t, map[string]string{"/": `<title>Hello</title><h1>Hi</h1>`})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, err := s.Open(ctx, srv.URL+"/", "", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if info.Title != "Hello" || info.ID != "t1" {
		t.Fatalf("info = %+v", info)
	}
}

func findRef(t *testing.T, snap Snapshot, needle string) string {
	t.Helper()
	for _, line := range snap.Lines {
		if strings.Contains(line, needle) {
			start := strings.Index(line, "[e")
			end := strings.Index(line[start:], "]")
			if start >= 0 && end > 0 {
				return line[start+1 : start+end]
			}
		}
	}
	t.Fatalf("no line containing %q with a ref in:\n%s", needle, strings.Join(snap.Lines, "\n"))
	return ""
}

const formPage = `<!doctype html><title>Form</title>
<h1>Sign up</h1>
<label>Email <input id="email" type="email"></label>
<label>Plan <select id="plan"><option value="free">Free</option><option value="pro">Pro</option></select></label>
<button id="go" onclick="document.getElementById('out').textContent = 'hello ' + document.getElementById('email').value + ' ' + document.getElementById('plan').value; console.error('boom')">Submit</button>
<p id="out"></p>
<div style="position:relative;height:40px"><button id="under">Hidden</button><div style="position:absolute;inset:0;background:#fff" class="veil"></div></div>
<a href="/next">Next page</a>
<button onclick="window.open('/next')">Pop</button>
<button onclick="document.getElementById('out').textContent = confirm('sure?') ? 'yes' : 'no'">Ask</button>`

func TestLiveSnapshotAndAct(t *testing.T) {
	s := liveSession(t, true)
	srv := liveServer(t, map[string]string{"/": formPage, "/next": `<title>Next</title><h1>Arrived</h1>`})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snap, err := s.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	t.Logf("snapshot:\n%s", strings.Join(snap.Lines, "\n"))
	email := findRef(t, snap, `textbox "Email"`)
	plan := findRef(t, snap, `combobox "Plan"`)
	submit := findRef(t, snap, `button "Submit"`)

	res, err := actPlain(ctx, s, "", []Step{
		{Action: "fill", Ref: email, Text: "李雷@example.com"},
		{Action: "select", Ref: plan, Values: []string{"Pro"}},
		{Action: "click", Ref: submit},
		{Action: "wait_for", Text: "hello 李雷@example.com pro"},
	})
	if err != nil {
		t.Fatalf("Act: %v (done %d, notes %v)", err, res.Done, res.Notes)
	}
	if len(res.Logs) == 0 || !strings.Contains(res.Logs[0].Text, "boom") {
		t.Fatalf("the console error did not come back with the act: %+v", res.Logs)
	}

	hidden := findRef(t, snap, `button "Hidden"`)
	_, err = actPlain(ctx, s, "", []Step{{Action: "click", Ref: hidden}})
	if CodeOf(err) != CodeCovered || !strings.Contains(err.Error(), "veil") {
		t.Fatalf("clicking under a veil = %v, want %s naming the veil", err, CodeCovered)
	}

	ask := findRef(t, snap, `button "Ask"`)
	res, err = actPlain(ctx, s, "", []Step{{Action: "click", Ref: ask}})
	if err != nil || res.Dialog == nil || res.Dialog.Type != "confirm" {
		t.Fatalf("confirm dialog: err=%v dialog=%+v", err, res.Dialog)
	}
	_, err = actPlain(ctx, s, "", []Step{{Action: "click", Ref: submit}})
	if CodeOf(err) != CodeDialogOpen {
		t.Fatalf("acting under an open dialog = %v, want %s", err, CodeDialogOpen)
	}
	no := false
	if _, err = actPlain(ctx, s, "", []Step{{Action: "dialog", Accept: &no}, {Action: "wait_for", Text: "no"}}); err != nil {
		t.Fatalf("dismiss dialog: %v", err)
	}

	pop := findRef(t, snap, `button "Pop"`)
	res, err = actPlain(ctx, s, "", []Step{{Action: "click", Ref: pop}, {Action: "wait", Ms: 500}})
	if err != nil || len(res.Opened) != 1 {
		t.Fatalf("popup: err=%v opened=%v", err, res.Opened)
	}
	if err := s.CloseTab(ctx, res.Opened[0]); err != nil {
		t.Fatalf("close popup: %v", err)
	}

	next := findRef(t, snap, `link "Next page"`)
	res, err = actPlain(ctx, s, "t1", []Step{{Action: "click", Ref: next}})
	if err != nil || !strings.HasSuffix(res.Tab.URL, "/next") || res.Tab.Title != "Next" {
		t.Fatalf("link: err=%v tab=%+v", err, res.Tab)
	}
	_, err = actPlain(ctx, s, "t1", []Step{{Action: "click", Ref: submit}})
	if CodeOf(err) != CodeStaleRef {
		t.Fatalf("a ref from the previous page = %v, want %s", err, CodeStaleRef)
	}
	res, err = actPlain(ctx, s, "t1", []Step{{Action: "back"}})
	if err != nil || strings.HasSuffix(res.Tab.URL, "/next") {
		t.Fatalf("back: err=%v tab=%+v", err, res.Tab)
	}
}

func TestLiveScreenshotCoordinatesLandOnTheElement(t *testing.T) {
	s := liveSession(t, true)
	srv := liveServer(t, map[string]string{"/": `<title>Canvas</title><body style="margin:0">
<div style="position:absolute;left:600px;top:400px;width:80px;height:60px;background:red" onclick="document.title='hit'"></div>`})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	url, _, err := s.Screenshot(ctx, "")
	if err != nil || !strings.HasPrefix(url, "data:image/") {
		t.Fatalf("Screenshot: %v", err)
	}
	scale := s.activeTab().screenshotScale()
	x, y := 640/scale, 430/scale
	res, err := actPlain(ctx, s, "", []Step{{Action: "click", X: &x, Y: &y}})
	if err != nil || res.Tab.Title != "hit" {
		t.Fatalf("coordinate click: err=%v title=%q scale=%v", err, res.Tab.Title, scale)
	}
}

func TestLiveRefusesFilesOutsideTheWorkspace(t *testing.T) {
	s := liveSession(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, "file:///etc/hosts", "", false); CodeOf(err) != CodeURLRefused {
		t.Fatalf("file outside the workspace = %v, want %s", err, CodeURLRefused)
	}
	if _, err := s.Open(ctx, "chrome://settings", "", false); CodeOf(err) != CodeURLRefused {
		t.Fatalf("browser page = %v, want %s", err, CodeURLRefused)
	}
}

// The port endpoint is what Windows uses; driving it here keeps that path
// exercised on machines that can run the pipe.
func TestLiveWebSocketTransport(t *testing.T) {
	was := usePipe
	usePipe = false
	t.Cleanup(func() { usePipe = was })
	s := liveSession(t, true)
	srv := liveServer(t, map[string]string{"/": `<title>Socket</title><button>Go</button>`})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, err := s.Open(ctx, srv.URL+"/", "", false)
	if err != nil || info.Title != "Socket" {
		t.Fatalf("Open over the port endpoint: info=%+v err=%v", info, err)
	}
	snap, err := s.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	findRef(t, snap, `button "Go"`)
}

func TestLiveDownloadsAreRefusedAndReported(t *testing.T) {
	s := liveSession(t, true)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>Files</title><a href="/report.csv" download>Get report</a>`))
	})
	mux.HandleFunc("/report.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="report.csv"`)
		_, _ = w.Write([]byte("a,b\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snap, err := s.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	link := findRef(t, snap, `link "Get report"`)
	res, err := actPlain(ctx, s, "", []Step{{Action: "click", Ref: link}, {Action: "wait", Ms: 800}})
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	for _, e := range res.Logs {
		if e.Kind == "download" && strings.Contains(e.Text, "report.csv") {
			return
		}
	}
	t.Fatalf("the refused download was not reported: %+v", res.Logs)
}

func actPlain(ctx context.Context, s *Session, tab string, steps []Step) (ActResult, error) {
	return s.Act(ctx, tab, steps, false, "")
}

func TestLiveSecretsNeedConfirmation(t *testing.T) {
	s := liveSession(t, true)
	srv := liveServer(t, map[string]string{"/": `<title>Login</title>
<label>Email <input type="email" autocomplete="username"></label>
<label>Password <input type="password" autocomplete="current-password"></label>
<label>Card <input autocomplete="cc-number"></label>
<button>Sign in</button>`})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snap, err := s.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	email, password := findRef(t, snap, `textbox "Email"`), findRef(t, snap, `textbox "Password"`)
	card := findRef(t, snap, `textbox "Card"`)
	for _, tc := range []struct {
		name  string
		steps []Step
		want  bool
	}{
		{"email", []Step{{Action: "fill", Ref: email, Text: "a@b.c"}}, false},
		{"password", []Step{{Action: "fill", Ref: email, Text: "a@b.c"}, {Action: "fill", Ref: password, Text: "x"}}, true},
		{"card", []Step{{Action: "type", Ref: card, Text: "4242"}}, true},
		{"typed after a click", []Step{{Action: "click", Ref: password}, {Action: "type", Text: "x"}}, true},
		{"typed where Tab left focus", []Step{{Action: "fill", Ref: email, Text: "a"}, {Action: "press", Key: "Tab"}, {Action: "type", Text: "x"}}, true},
		{"typed after clicking the email", []Step{{Action: "click", Ref: email}, {Action: "type", Text: "x"}}, false},
	} {
		if got := s.CredentialEntry(ctx, "", tc.steps); got != tc.want {
			t.Errorf("%s: CredentialEntry = %v, want %v", tc.name, got, tc.want)
		}
	}

	_, err = s.Act(ctx, "", []Step{{Action: "fill", Ref: email, Text: "a@b.c"}, {Action: "press", Key: "Tab"}, {Action: "type", Text: "hunter2"}}, false, "")
	if CodeOf(err) != CodeUnconfirmedSecret {
		t.Fatalf("typing into the password field without confirmation = %v, want %s", err, CodeUnconfirmedSecret)
	}
	if _, err := s.Act(ctx, "", []Step{{Action: "fill", Ref: password, Text: "hunter2"}}, true, ""); err != nil {
		t.Fatalf("confirmed secret entry: %v", err)
	}
}

// A link to a server that takes a while to answer has started navigating long
// before the new document commits; the act answers with the page it led to.
func TestLiveAClickWaitsForTheSlowNavigationItStarted(t *testing.T) {
	s := liveSession(t, true)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<title>Start</title><a href="/slow">Slow page</a>`))
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<title>Slow</title><h1>Arrived late</h1>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snap, err := s.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	res, err := actPlain(ctx, s, "", []Step{{Action: "click", Ref: findRef(t, snap, `link "Slow page"`)}})
	if err != nil || !strings.HasSuffix(res.Tab.URL, "/slow") || res.Tab.Title != "Slow" {
		t.Fatalf("slow link: err=%v tab=%+v", err, res.Tab)
	}
}

// The four a browser task reaches for that a snapshot cannot stand in for: the
// page served again after its file changed, the way back and forward through
// this tab's own history, a drag, and handing a file input a file.
func TestLiveReloadHistoryDragAndUpload(t *testing.T) {
	s := liveSession(t, true)
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("from the workspace"), 0o644); err != nil {
		t.Fatal(err)
	}
	served := "first"
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<title>Bench</title><h1 id="served">%s</h1>
<a href="/next">Next page</a>
<input id="file" type="file" aria-label="Pick a file">
<div id="from" role="button" aria-label="Handle" style="width:80px;height:80px;background:#ccc">drag me</div>
<div id="to" role="button" aria-label="Target" style="width:120px;height:120px;background:#eee">drop here</div>
<p id="said"></p>
<script>
  document.getElementById('file').addEventListener('change', e => { document.getElementById('said').textContent = 'file: ' + e.target.files[0].name });
  const to = document.getElementById('to');
  let down = false;
  document.getElementById('from').addEventListener('mousedown', () => { down = true });
  to.addEventListener('mouseup', () => { if (down) document.getElementById('said').textContent = 'dropped'; down = false });
</script>`, served)
	})
	mux.HandleFunc("/next", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<title>Next</title><h1>the next page</h1>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sess := NewSession(Config{Launch: LaunchSpec{Executable: s.cfg.Launch.Executable, ProfileDir: testenv.TempDir(t), Headless: true}, Roots: []string{root}})
	t.Cleanup(sess.Close)
	if _, err := sess.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snap, err := sess.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// The page is served again from the server, so a change behind it shows.
	served = "second"
	res, err := actPlain(ctx, sess, "", []Step{{Action: "reload"}, {Action: "wait_for", Text: "second"}})
	if err != nil {
		t.Fatalf("reload: %v (%v)", err, res.Notes)
	}

	// Back and forward walk this tab's own history.
	after, err := sess.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := actPlain(ctx, sess, "", []Step{{Action: "click", Ref: findRef(t, after, `link "Next page"`)}}); err != nil {
		t.Fatalf("link: %v", err)
	}
	res, err = actPlain(ctx, sess, "", []Step{{Action: "back"}})
	if err != nil || strings.HasSuffix(res.Tab.URL, "/next") {
		t.Fatalf("back: %v %+v", err, res.Tab)
	}
	res, err = actPlain(ctx, sess, "", []Step{{Action: "forward"}})
	if err != nil || !strings.HasSuffix(res.Tab.URL, "/next") {
		t.Fatalf("forward: %v %+v", err, res.Tab)
	}
	if _, err := actPlain(ctx, sess, "", []Step{{Action: "back"}}); err != nil {
		t.Fatalf("back again: %v", err)
	}

	// A file input takes a file inside the workspace and nothing else.
	now, err := sess.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	file := findRef(t, now, `button "Pick a file"`)
	if _, err := actPlain(ctx, sess, "", []Step{
		{Action: "upload", Ref: file, Files: []string{filepath.Join(root, "note.txt")}},
		{Action: "wait_for", Text: "file: note.txt"},
	}); err != nil {
		t.Fatalf("upload: %v", err)
	}
	_, err = actPlain(ctx, sess, "", []Step{{Action: "upload", Ref: file, Files: []string{"/etc/hosts"}}})
	if CodeOf(err) != CodeURLRefused {
		t.Fatalf("a file outside the workspace = %v, want %s", err, CodeURLRefused)
	}

	// A drag travels rather than jumping, so a page that follows the pointer
	// sees it.
	if _, err := actPlain(ctx, sess, "", []Step{
		{Action: "drag", Ref: findRef(t, now, `button "Handle"`), ToRef: findRef(t, now, `button "Target"`)},
		{Action: "wait_for", Text: "dropped"},
	}); err != nil {
		t.Fatalf("drag: %v", err)
	}
	_ = snap
}

// What a page asked the network for, and the viewport it was laid out in, both
// come from the browser rather than from us — so both are read from a real one.
func TestLiveTheNetworkListAndTheViewport(t *testing.T) {
	s := liveSession(t, true)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>Wide</title><script src="/app.js"></script>
<p id="size"></p><div style="width:1600px">wide</div><script>
fetch("/api/ok").then(() => fetch("/api/missing"));
const show = () => document.getElementById("size").textContent = "viewport " + innerWidth + "x" + innerHeight;
addEventListener("resize", show); show();
</script>`))
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte("// a script the page asked for\n"))
	})
	mux.HandleFunc("/api/ok", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/missing", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := actPlain(ctx, s, "", []Step{{Action: "wait", Ms: 1200}}); err != nil {
		t.Fatalf("wait: %v", err)
	}

	requests, _, err := s.Requests("")
	if err != nil {
		t.Fatalf("Requests: %v", err)
	}
	found := map[string]Request{}
	for _, r := range requests {
		found[strings.TrimPrefix(r.URL, srv.URL)] = r
	}
	page, ok := found["/"]
	if !ok || page.Status != 200 || page.Kind != "document" || page.Bytes == 0 {
		t.Fatalf("the page's own request = %+v (of %d)", page, len(requests))
	}
	if js, ok := found["/app.js"]; !ok || js.Status != 200 || js.Kind != "script" {
		t.Errorf("the script = %+v", js)
	}
	if api, ok := found["/api/ok"]; !ok || api.Status != 200 || !strings.Contains(api.Mime, "json") {
		t.Errorf("the call that worked = %+v", api)
	}
	if gone, ok := found["/api/missing"]; !ok || gone.Status != 404 {
		t.Errorf("the call that 404ed = %+v", gone)
	}

	// A navigation does not clear the list, and what came before it says so.
	if _, err := s.Open(ctx, srv.URL+"/api/ok", "", false); err != nil {
		t.Fatalf("Open again: %v", err)
	}
	after, _, err := s.Requests("")
	if err != nil {
		t.Fatalf("Requests: %v", err)
	}
	if len(after) <= len(requests) {
		t.Fatalf("the list lost what the tab had asked for: %d then %d", len(requests), len(after))
	}
	if first, last := after[0].Page, after[len(after)-1].Page; first == last {
		t.Fatalf("every request claims the same document across a navigation: %d", first)
	}

	if _, err := s.Open(ctx, srv.URL+"/", "", false); err != nil {
		t.Fatalf("Open the page again: %v", err)
	}
	if _, err := actPlain(ctx, s, "", []Step{{Action: "resize", Width: 390, Height: 844}, {Action: "wait", Ms: 400}}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	snap, err := s.Snapshot(ctx, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.Contains(strings.Join(snap.Lines, "\n"), "viewport 390x844") {
		t.Fatalf("the page was not laid out in the viewport it was given:\n%s", strings.Join(snap.Lines, "\n"))
	}

	// A page too wide for its viewport is read by scrolling across it.
	across := func() float64 {
		var r struct {
			Result struct{ Value float64 } `json:"result"`
		}
		if err := s.tabs[0].call(ctx, "Runtime.evaluate", map[string]any{"expression": "scrollX", "returnByValue": true}, &r); err != nil {
			t.Fatalf("scrollX: %v", err)
		}
		return r.Result.Value
	}
	if at := across(); at != 0 {
		t.Fatalf("the page starts scrolled across at %v", at)
	}
	res, err := actPlain(ctx, s, "", []Step{{Action: "scroll", DeltaX: 200}, {Action: "wait", Ms: 300}})
	if err != nil || !strings.Contains(res.Notes[0], "sideways") {
		t.Fatalf("scroll across = %+v, %v", res.Notes, err)
	}
	if at := across(); at <= 0 {
		t.Fatalf("the page did not scroll across: scrollX = %v", at)
	}
}

// A page the person closes is not a page the agent lost track of. The model is
// told which, or it reopens the window the person just shut, once per step.
func TestLiveAPageClosedOutsideTheAgentSaysSo(t *testing.T) {
	s := liveSession(t, true)
	srv := liveServer(t, map[string]string{"/": `<title>Hello</title><h1>Hi</h1>`})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, err := s.Open(ctx, srv.URL+"/", "", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.eng.conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": info.Target}, nil); err != nil {
		t.Fatalf("close from outside: %v", err)
	}
	for len(s.Tabs()) > 0 {
		if ctx.Err() != nil {
			t.Fatal("the closed page never left the session")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := s.Snapshot(ctx, "", ""); CodeOf(err) != CodeTabClosed {
		t.Fatalf("the active page after an outside close = %v, want %s", err, CodeTabClosed)
	}
	if _, err := s.Switch(info.ID); CodeOf(err) != CodeTabClosed {
		t.Fatalf("%s after an outside close = %v, want %s", info.ID, err, CodeTabClosed)
	}

	// The agent's own close is not someone else's.
	again, err := s.Open(ctx, srv.URL+"/", "", false)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := s.CloseTab(ctx, again.ID); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := s.Snapshot(ctx, "", ""); CodeOf(err) != CodeNoTab {
		t.Fatalf("after the agent closed its page = %v, want %s", err, CodeNoTab)
	}
}
