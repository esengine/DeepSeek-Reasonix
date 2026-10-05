package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// slashPath is p as a URL path: C:\x is /C:/x, the form a browser reports.
func slashPath(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func fileURL(p string) string { return (&url.URL{Scheme: "file", Path: slashPath(p)}).String() }

func TestCheckURL(t *testing.T) {
	root := testenv.TempDir(t)
	inside := filepath.Join(root, "index.html")
	if err := os.WriteFile(inside, []byte("<p>hi</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(testenv.TempDir(t), "secret.html")
	if err := os.WriteFile(outside, []byte("<p>no</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.html")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cases := []struct {
		raw  string
		want Code
	}{
		{"https://example.com/a?b=c", ""},
		{"http://127.0.0.1:5173/", ""},
		{"about:blank", ""},
		{fileURL(inside), ""},
		{fileURL(outside), CodeURLRefused},
		{fileURL(link), CodeURLRefused},
		{"file://localhost" + slashPath(inside), ""},
		{"file://elsewhere" + slashPath(inside), CodeURLRefused},
		{"file://elsewhere/share/index.html", CodeURLRefused},
		{"chrome://settings", CodeURLRefused},
		{"javascript:alert(1)", CodeURLRefused},
		{"data:text/html,<p>x</p>", CodeURLRefused},
		{"example.com", CodeURLRefused},
		{"https://", CodeURLRefused},
	}
	if runtime.GOOS == "windows" {
		// A bare drive resolves against the working directory; standing inside
		// the root is what would let it pass.
		t.Chdir(root)
		cases = append(cases, struct {
			raw  string
			want Code
		}{"file://" + filepath.ToSlash(inside), ""}, struct {
			raw  string
			want Code
		}{"file:///" + filepath.VolumeName(root), CodeURLRefused}, struct {
			raw  string
			want Code
		}{"file://" + filepath.VolumeName(root), CodeURLRefused})
	}
	for _, tc := range cases {
		_, err := checkURL(tc.raw, []string{root})
		if CodeOf(err) != tc.want {
			t.Errorf("checkURL(%q) = %v, want code %q", tc.raw, err, tc.want)
		}
	}
}

func TestParseKey(t *testing.T) {
	cases := []struct {
		chord     string
		key, text string
		modifiers int
		ok        bool
	}{
		{"Enter", "Enter", "\r", 0, true},
		{"shift+tab", "Tab", "", 8, true},
		{"Control+a", "a", "", 2, true},
		{"x", "x", "x", 0, true},
		{"Meta+Shift+Z", "Z", "", 12, true},
		{"Hyper+a", "", "", 0, false},
		{"Enterr", "", "", 0, false},
	}
	for _, tc := range cases {
		def, modifiers, ok := parseKey(tc.chord)
		if ok != tc.ok || (ok && (def.key != tc.key || def.text != tc.text || modifiers != tc.modifiers)) {
			t.Errorf("parseKey(%q) = %+v, %d, %v", tc.chord, def, modifiers, ok)
		}
	}
}

func TestRefsAreNeverReusedAndRetireWithTheirDocument(t *testing.T) {
	var r refTable
	a := r.refFor("t1", 10)
	if again := r.refFor("t1", 10); again != a {
		t.Fatalf("the same element got %s then %s", a, again)
	}
	other := r.refFor("t2", 10)
	if other == a {
		t.Fatal("a node id on another tab reused a ref")
	}
	r.retireTab("t1")
	if _, err := r.resolve(a); CodeOf(err) != CodeStaleRef {
		t.Fatalf("a retired ref resolved as %v, want %s", err, CodeStaleRef)
	}
	if fresh := r.refFor("t1", 10); fresh == a {
		t.Fatal("a node on the next document reused a retired ref")
	}
	if _, err := r.resolve(other); err != nil {
		t.Fatalf("retiring t1 touched t2: %v", err)
	}
	if _, err := r.resolve("e999"); CodeOf(err) != CodeUnknownRef {
		t.Fatalf("an unissued ref resolved as %v, want %s", err, CodeUnknownRef)
	}
}

func axJSON(t *testing.T, nodes string) []axNode {
	t.Helper()
	var out []axNode
	if err := json.Unmarshal([]byte(nodes), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSnapshotRendering(t *testing.T) {
	nodes := axJSON(t, `[
		{"nodeId":"1","role":{"value":"RootWebArea"},"name":{"value":"Page"},"childIds":["2","6","9"],"backendDOMNodeId":1},
		{"nodeId":"2","parentId":"1","role":{"value":"generic"},"childIds":["3","4"],"backendDOMNodeId":2},
		{"nodeId":"3","parentId":"2","role":{"value":"StaticText"},"name":{"value":"Hello"},"childIds":["5"]},
		{"nodeId":"5","parentId":"3","role":{"value":"InlineTextBox"},"name":{"value":"Hello"}},
		{"nodeId":"4","parentId":"2","role":{"value":"StaticText"},"name":{"value":"world"}},
		{"nodeId":"6","parentId":"1","role":{"value":"button"},"name":{"value":"Save"},"childIds":["7"],"backendDOMNodeId":6,
		 "properties":[{"name":"disabled","value":{"value":true}},{"name":"focusable","value":{"value":true}}]},
		{"nodeId":"7","parentId":"6","role":{"value":"StaticText"},"name":{"value":"Save"}},
		{"nodeId":"9","parentId":"1","ignored":true,"role":{"value":"none"},"childIds":["10"]},
		{"nodeId":"10","parentId":"9","role":{"value":"textbox"},"name":{"value":"Email"},"value":{"value":"a@b.c"},"backendDOMNodeId":10,
		 "properties":[{"name":"focused","value":{"value":true}}]}
	]`)
	s := NewSession(Config{})
	r := renderer{s: s, tab: "t1", nodes: map[string]*axNode{}}
	for i := range nodes {
		r.nodes[nodes[i].NodeID] = &nodes[i]
	}
	r.children(&nodes[0], 0, "Page")
	got := strings.Join(r.lines, "\n")
	want := strings.Join([]string{
		`- text: "Hello world"`,
		`- button "Save" [e1] disabled`,
		`- textbox "Email" [e2] value="a@b.c" focused`,
	}, "\n")
	if got != want {
		t.Fatalf("rendering:\n%s\nwant:\n%s", got, want)
	}
}

// fakeTransport answers commands from a script and can push events.
type fakeTransport struct {
	in     chan []byte
	answer func(msg wireMessage) []byte
	closed chan struct{}
}

func newFakeTransport(answer func(wireMessage) []byte) *fakeTransport {
	return &fakeTransport{in: make(chan []byte, 16), answer: answer, closed: make(chan struct{})}
}

func (f *fakeTransport) read() ([]byte, error) {
	select {
	case msg := <-f.in:
		return msg, nil
	case <-f.closed:
		return nil, io.EOF
	}
}

func (f *fakeTransport) write(raw []byte) error {
	var msg wireMessage
	_ = json.Unmarshal(raw, &msg)
	if reply := f.answer(msg); reply != nil {
		f.in <- reply
	}
	return nil
}

func (f *fakeTransport) close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

func TestConnRoutesRepliesErrorsAndEvents(t *testing.T) {
	ft := newFakeTransport(func(msg wireMessage) []byte {
		switch msg.Method {
		case "Ok.method":
			b, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{"value": 7}})
			return b
		case "Bad.method":
			b, _ := json.Marshal(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32000, "message": "No node"}})
			return b
		}
		return nil
	})
	c := newConn(ft)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out struct {
		Value int `json:"value"`
	}
	if err := c.call(ctx, "", "Ok.method", nil, &out); err != nil || out.Value != 7 {
		t.Fatalf("call = %v, value %d", err, out.Value)
	}
	if err := c.call(ctx, "S1", "Bad.method", nil, nil); !isProtocolError(err) {
		t.Fatalf("a protocol error came back as %v", err)
	}

	got := make(chan string, 2)
	stop := c.subscribe("S1", func(ev event) { got <- ev.Method })
	defer stop()
	ft.in <- []byte(`{"method":"Other.event","sessionId":"S2"}`)
	ft.in <- []byte(`{"method":"Page.loadEventFired","sessionId":"S1"}`)
	select {
	case m := <-got:
		if m != "Page.loadEventFired" {
			t.Fatalf("session S1 received %s", m)
		}
	case <-ctx.Done():
		t.Fatal("the event never arrived")
	}

	pending := make(chan error, 1)
	go func() { pending <- c.call(ctx, "", "Never.answered", nil, nil) }()
	time.Sleep(50 * time.Millisecond)
	_ = ft.close()
	if err := <-pending; !errors.Is(err, errConnClosed) {
		t.Fatalf("a call pending when the browser went away returned %v", err)
	}
}

func TestPipeTransportFramesOnNUL(t *testing.T) {
	r, w := io.Pipe()
	p := newPipeTransport(r, w)
	go func() {
		_ = p.write([]byte(`{"id":1}`))
		_ = p.write([]byte(`{"id":2}`))
	}()
	for _, want := range []string{`{"id":1}`, `{"id":2}`} {
		got, err := p.read()
		if err != nil || string(got) != want {
			t.Fatalf("read = %q, %v; want %q", got, err, want)
		}
	}
}

func TestReadActivePort(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "DevToolsActivePort")
	if _, _, ok := readActivePort(path); ok {
		t.Fatal("a missing file read as a port")
	}
	if err := os.WriteFile(path, []byte("9222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := readActivePort(path); ok {
		t.Fatal("a half-written file read as a port")
	}
	if err := os.WriteFile(path, []byte("9222\n/devtools/browser/abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if port, wsPath, ok := readActivePort(path); !ok || port != "9222" || wsPath != "/devtools/browser/abc" {
		t.Fatalf("readActivePort = %q %q %v", port, wsPath, ok)
	}
}

func TestDiscoverUsesOnlyTheConfiguredBrowserWhenSet(t *testing.T) {
	if _, err := Discover(filepath.Join(testenv.TempDir(t), "nope")); CodeOf(err) != CodeEngineMissing {
		t.Fatalf("a configured path that does not exist = %v, want %s", err, CodeEngineMissing)
	}
	env := map[string]string{"HOME": "/Users/me", "ProgramFiles": `C:\Program Files`}
	get := func(k string) string { return env[k] }
	if mac := installedCandidates("darwin", get); !strings.HasSuffix(mac[0], "Google Chrome.app/Contents/MacOS/Google Chrome") {
		t.Fatalf("darwin candidates start with %q", mac[0])
	}
	if win := installedCandidates("windows", get); len(win) != 2 || !strings.HasSuffix(win[1], `msedge.exe`) {
		t.Fatalf("windows candidates = %v", win)
	}
}

func TestDiffLinesListsWhatAppearedAndWhatLeft(t *testing.T) {
	before := []string{"- a", "- b", "- b", "- c"}
	after := []string{"- a", "- b", "- d", "- c", "- b"}
	c := diffLines(before, after)
	if strings.Join(c.Added, "|") != "- d" || len(c.Removed) != 0 {
		t.Fatalf("changes = %+v", c)
	}
	c = diffLines(after, before)
	if len(c.Added) != 0 || strings.Join(c.Removed, "|") != "- d" {
		t.Fatalf("reverse changes = %+v", c)
	}
}

func TestSecretMarkup(t *testing.T) {
	cases := []struct {
		name  string
		attrs []string
		want  bool
	}{
		{"input", []string{"type", "password"}, true},
		{"input", []string{"type", "PASSWORD"}, true},
		{"input", []string{"type", "email", "autocomplete", "username"}, false},
		{"input", []string{"autocomplete", "shipping cc-number"}, true},
		{"input", []string{"autocomplete", "one-time-code"}, true},
		{"textarea", []string{"type", "password"}, false},
		{"iframe", nil, true},
		{"div", []string{"contenteditable", "true"}, false},
	}
	for _, tc := range cases {
		if got := secretMarkup(tc.name, tc.attrs); got != tc.want {
			t.Errorf("secretMarkup(%s %v) = %v, want %v", tc.name, tc.attrs, got, tc.want)
		}
	}
}

func TestWhichPagesAreTheWorkspacesOwn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<title>x</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewSession(Config{Roots: []string{root}})
	cases := map[string]bool{
		"http://127.0.0.1:8123/index.html":         true,
		"http://localhost:5173/":                   true,
		"https://[::1]:8443/app":                   true,
		fileURL(filepath.Join(root, "index.html")): true,
		"file:///etc/passwd":                       false,
		"https://example.com/":                     false,
		"about:blank":                              false,
		"":                                         false,
	}
	for raw, want := range cases {
		if got := s.ServesWorkspace(raw); got != want {
			t.Errorf("ServesWorkspace(%q) = %v, want %v", raw, got, want)
		}
	}
}

// The window this browser runs in injects its own bundle into every page and
// warns there about the page's security headers. The model read that as the
// page speaking, on every page it opened.
func TestAMessageOnlyTheHostsOwnScriptProducedIsNotThePages(t *testing.T) {
	frames := func(urls ...string) stack {
		var s stack
		for _, u := range urls {
			s.Frames = append(s.Frames, struct {
				URL string `json:"url"`
			}{URL: u})
		}
		return s
	}
	cases := []struct {
		name string
		in   stack
		want bool
	}{
		{"electron's own bundle", frames("node:electron/js2c/sandbox_bundle"), true},
		{"several frames of it", frames("node:electron/js2c/sandbox_bundle", "node:electron/js2c/renderer_init"), true},
		{"an extension", frames("chrome-extension://abc/content.js"), true},
		{"the page's own script", frames("http://127.0.0.1:8123/app.js"), false},
		{"a page frame under an injected one", frames("node:electron/js2c/sandbox_bundle", "https://example.com/a.js"), false},
		{"code with no script url", frames(""), false},
		{"no stack at all", stack{}, false},
	}
	for _, tc := range cases {
		if got := tc.in.hostInjected(); got != tc.want {
			t.Errorf("%s: hostInjected() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// netEvent feeds the tab one Network event the way the engine would.
func netEvent(t *testing.T, tb *tab, method string, params any) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	tb.onNetwork(event{Method: method, Params: raw})
}

func TestTheNetworkListSaysWhatWasAskedForAndWhatCameBack(t *testing.T) {
	_, tb := newFakeSession(t, &fakePage{})
	sent := func(id, method, url, kind string, at float64) {
		netEvent(t, tb, "Network.requestWillBeSent", map[string]any{
			"requestId": id, "type": kind, "timestamp": at,
			"request": map[string]any{"method": method, "url": url},
		})
	}
	sent("1", "GET", "https://example.com/app.js", "Script", 10)
	netEvent(t, tb, "Network.responseReceived", map[string]any{
		"requestId": "1", "response": map[string]any{"status": 200, "mimeType": "text/javascript"},
	})
	netEvent(t, tb, "Network.loadingFinished", map[string]any{"requestId": "1", "timestamp": 10.25, "encodedDataLength": 4096})

	sent("2", "POST", "https://example.com/api/save", "XHR", 11)
	netEvent(t, tb, "Network.responseReceived", map[string]any{
		"requestId": "2", "response": map[string]any{"status": 500, "mimeType": "application/json", "url": "https://example.com/api/save"},
	})
	sent("3", "GET", "https://example.com/gone", "Fetch", 12)
	netEvent(t, tb, "Network.loadingFailed", map[string]any{"requestId": "3", "timestamp": 12.5, "errorText": "net::ERR_NAME_NOT_RESOLVED"})
	sent("4", "GET", "https://example.com/slow", "Fetch", 13)

	got := make([]*Request, 0, 4)
	tb.mu.Lock()
	got = append(got, tb.netlog...)
	tb.mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("the tab remembered %d requests, want 4", len(got))
	}
	if got[0].Status != 200 || got[0].Kind != "script" || got[0].Bytes != 4096 || got[0].Millis != 250 {
		t.Errorf("a request that finished = %+v", *got[0])
	}
	// A response that arrives is enough to say what came back; the bytes and
	// the time only settle when the browser says the loading finished.
	if got[1].Status != 500 || got[1].Millis != 0 {
		t.Errorf("a request answered but not finished = %+v", *got[1])
	}
	if got[2].Failed != "net::ERR_NAME_NOT_RESOLVED" || got[2].Millis != 500 {
		t.Errorf("a request that failed = %+v", *got[2])
	}
	if got[3].Status != 0 || got[3].Failed != "" {
		t.Errorf("a request still in flight = %+v", *got[3])
	}
	// The page said two things worth a person's attention, and nothing about
	// the two that went as asked.
	entries, _ := tb.logsAfter(0)
	if len(entries) != 2 {
		t.Fatalf("the page reported %d things, want the 500 and the failure: %+v", len(entries), entries)
	}
}

func TestATabRemembersOnlyTheRecentRequests(t *testing.T) {
	_, tb := newFakeSession(t, &fakePage{})
	for i := range maxRequests + 50 {
		netEvent(t, tb, "Network.requestWillBeSent", map[string]any{
			"requestId": fmt.Sprint(i), "timestamp": float64(i),
			"request": map[string]any{"method": "GET", "url": fmt.Sprintf("https://example.com/%d", i)},
		})
	}
	tb.mu.Lock()
	kept, indexed := len(tb.netlog), len(tb.requests)
	oldest := tb.netlog[0].URL
	tb.mu.Unlock()
	if kept != maxRequests || indexed != maxRequests {
		t.Fatalf("kept %d requests and %d index entries, want %d of each", kept, indexed, maxRequests)
	}
	if oldest != "https://example.com/50" {
		t.Fatalf("the oldest kept request is %q, want the 50th", oldest)
	}
	// What was evicted is no longer named in the log either.
	if name := tb.requestName("0", "unknown"); name != "unknown" {
		t.Fatalf("an evicted request is still named %q", name)
	}
}

func TestAViewportIsAskedForInPixelsWithinReason(t *testing.T) {
	_, tb := newFakeSession(t, &fakePage{})
	ctx := context.Background()
	for _, step := range []Step{
		{Width: 0, Height: 800},
		{Width: 390, Height: 0},
		{Width: 100, Height: 800},
		{Width: 390, Height: 9000},
	} {
		if _, err := tb.resize(ctx, step); CodeOf(err) != CodeBadStep {
			t.Errorf("resize %dx%d = %v, want %s", step.Width, step.Height, err, CodeBadStep)
		}
	}
	note, err := tb.resize(ctx, Step{Width: 390, Height: 844})
	if err != nil || note != "resize to 390×844" {
		t.Fatalf("resize = %q, %v", note, err)
	}
}
