package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestNormalizeBrowserURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "blank", raw: "", want: "about:blank"},
		{name: "host shorthand", raw: "example.com/path", want: "https://example.com/path"},
		{name: "https", raw: "https://example.com/a?b=1", want: "https://example.com/a?b=1"},
		{name: "localhost", raw: "http://127.0.0.1:5173", want: "http://127.0.0.1:5173"},
		{name: "file blocked", raw: "file:///tmp/private", wantErr: true},
		{name: "script blocked", raw: "javascript:alert(1)", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBrowserURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeBrowserURL(%q) succeeded with %q", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBrowserURL(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeBrowserURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestLocalBrowserURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "localhost", url: "http://localhost:5173", want: true},
		{name: "localhost subdomain", url: "https://app.localhost:8443", want: true},
		{name: "ipv4 loopback", url: "http://127.12.3.4:3000", want: true},
		{name: "ipv6 loopback", url: "http://[::1]:5173", want: true},
		{name: "unspecified ipv4", url: "http://0.0.0.0:4173", want: true},
		{name: "unspecified ipv6", url: "http://[::]:4173", want: true},
		{name: "private lan", url: "http://192.168.1.10:5173", want: false},
		{name: "public", url: "https://example.com", want: false},
		{name: "lookalike", url: "https://localhost.example.com", want: false},
		{name: "blank", url: "about:blank", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalBrowserURL(tt.url); got != tt.want {
				t.Fatalf("isLocalBrowserURL(%q) = %t, want %t", tt.url, got, tt.want)
			}
		})
	}
}

func TestBrowserProfilePathUsesReasonixHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", root)
	got, err := browserProfilePath()
	if err != nil {
		t.Fatalf("browserProfilePath: %v", err)
	}
	want := filepath.Join(root, "browser", "profile")
	if got != want {
		t.Fatalf("browserProfilePath = %q, want %q", got, want)
	}
}

func TestResolveChromiumExecutableUsesDevelopmentOverride(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "chromium-test")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(executable, []byte("test"), 0o755); err != nil {
		t.Fatalf("write fake Chromium: %v", err)
	}
	t.Setenv(browserExecutableEnv, executable)
	got, err := resolveChromiumExecutable()
	if err != nil {
		t.Fatalf("resolveChromiumExecutable: %v", err)
	}
	want, _ := filepath.Abs(executable)
	if got != want {
		t.Fatalf("resolveChromiumExecutable = %q, want %q", got, want)
	}
}

func TestBrowserManagerCloseTabCancelsOwnedSession(t *testing.T) {
	app := NewApp()
	manager := app.browsers
	ctx, cancel := context.WithCancel(context.Background())
	session := &browserSession{
		manager: manager,
		tabID:   "tab-browser",
		ctx:     ctx,
		cancel:  cancel,
		ready:   make(chan struct{}),
		state: BrowserSessionView{
			TabID:  "tab-browser",
			PageID: "page-browser",
		},
	}
	manager.sessions[session.tabID] = session

	manager.closeForTab(session.tabID)
	if !session.closed.Load() {
		t.Fatal("browser session was not marked closed")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("browser session context was not cancelled")
	}
	if _, ok := manager.sessions[session.tabID]; ok {
		t.Fatal("browser session remained registered after closeForTab")
	}
	manager.closeForTab(session.tabID)
}

func TestBrowserManagerCloseForTabDoesNotCloseOtherSessions(t *testing.T) {
	app := NewApp()
	manager := app.browsers
	newSession := func(tabID string) *browserSession {
		ctx, cancel := context.WithCancel(context.Background())
		return &browserSession{
			manager: manager,
			tabID:   tabID,
			ctx:     ctx,
			cancel:  cancel,
			ready:   make(chan struct{}),
			state:   BrowserSessionView{TabID: tabID, PageID: "page-" + tabID},
		}
	}
	first := newSession("first")
	second := newSession("second")
	manager.sessions[first.tabID] = first
	manager.sessions[second.tabID] = second

	manager.closeForTab(first.tabID)
	if !first.closed.Load() {
		t.Fatal("owned session was not closed")
	}
	if second.closed.Load() || manager.sessions[second.tabID] != second {
		t.Fatal("closing one tab affected another browser session")
	}
	manager.shutdown()
}

func TestBrowserSessionWaitingForStartupUnblocksWhenTabCloses(t *testing.T) {
	app := NewApp()
	manager := app.browsers
	ctx, cancel := context.WithCancel(context.Background())
	session := &browserSession{
		manager: manager,
		tabID:   "starting",
		ctx:     ctx,
		cancel:  cancel,
		ready:   make(chan struct{}),
		state:   BrowserSessionView{TabID: "starting"},
	}
	manager.sessions[session.tabID] = session
	done := make(chan error, 1)
	go func() {
		_, err := session.waitReady()
		done <- err
	}()

	manager.closeForTab(session.tabID)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("startup waiter succeeded after its tab was closed")
		}
	case <-time.After(time.Second):
		t.Fatal("startup waiter remained blocked after tab close")
	}
}

func TestBrowserManagerLostConnectionClearsGeneration(t *testing.T) {
	app := NewApp()
	manager := app.browsers
	ctx, cancel := context.WithCancel(context.Background())
	session := &browserSession{
		manager:    manager,
		tabID:      "lost",
		ctx:        ctx,
		cancel:     cancel,
		generation: 4,
		ready:      make(chan struct{}),
		state:      BrowserSessionView{TabID: "lost", PageID: "page-lost"},
	}
	manager.sessions[session.tabID] = session
	manager.generation = 4
	manager.browserContext = context.Background()
	lost := make(chan struct{})
	done := make(chan struct{})
	go func() {
		manager.watchBrowser(4, manager.browserContext, lost)
		close(done)
	}()
	close(lost)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lost connection cleanup did not finish")
	}
	if !session.closed.Load() {
		t.Fatal("lost connection did not close the session")
	}
	if manager.browserContext != nil || len(manager.sessions) != 0 || manager.generation != 5 {
		t.Fatalf("manager remained attached after loss: generation=%d sessions=%d", manager.generation, len(manager.sessions))
	}
	manager.shutdown()
	manager.shutdown()
}

func TestBundledChromiumCandidatesMatchPackageLayouts(t *testing.T) {
	base := filepath.Join("opt", "Reasonix")
	windows := bundledChromiumCandidates(base, "windows", "arm64")
	if len(windows) != 1 || windows[0] != filepath.Join(base, "chromium", "chrome.exe") {
		t.Fatalf("Windows candidates = %#v", windows)
	}
	mac := bundledChromiumCandidates(base, "darwin", "arm64")
	macSuffix := filepath.Join("Resources", "chromium", "arm64", "Chromium.app", "Contents", "MacOS", "Google Chrome for Testing")
	if len(mac) != 1 || !strings.HasSuffix(filepath.Clean(mac[0]), macSuffix) {
		t.Fatalf("macOS candidates = %#v", mac)
	}
	linux := bundledChromiumCandidates(base, "linux", "amd64")
	if len(linux) != 2 || linux[0] != filepath.Join(base, "chromium", "chrome") || filepath.ToSlash(linux[1]) != "/usr/lib/reasonix/chromium/chrome" {
		t.Fatalf("Linux candidates = %#v", linux)
	}
	dev := developmentChromiumCandidates(base, "linux", "arm64")
	if len(dev) != 2 || !strings.Contains(filepath.ToSlash(dev[0]), "build/runtime/chromium/linux-arm64/chrome") {
		t.Fatalf("development candidates = %#v", dev)
	}
}

func TestClampBrowserViewport(t *testing.T) {
	width, height := clampBrowserViewport(100, 9000)
	if width != browserDefaultWidth || height != 4096 {
		t.Fatalf("clampBrowserViewport = %dx%d, want %dx4096", width, height, browserDefaultWidth)
	}
}

func TestNormalizeBrowserStyleOverrides(t *testing.T) {
	styles, err := normalizeBrowserStyleOverrides(map[string]string{
		" color ":       " #d97757 ",
		"border-radius": "10px",
		"opacity":       "",
	})
	if err != nil {
		t.Fatalf("normalizeBrowserStyleOverrides: %v", err)
	}
	if styles["color"] != "#d97757" || styles["border-radius"] != "10px" {
		t.Fatalf("normalizeBrowserStyleOverrides = %#v", styles)
	}
	if _, ok := styles["opacity"]; ok {
		t.Fatalf("empty browser style was retained: %#v", styles)
	}
	if _, err := normalizeBrowserStyleOverrides(map[string]string{"position": "fixed"}); err == nil {
		t.Fatal("unsupported browser style was accepted")
	}
	if _, err := normalizeBrowserStyleOverrides(map[string]string{"color": "red; display:none"}); err == nil {
		t.Fatal("browser style injection was accepted")
	}
}

func TestValidateBrowserSelector(t *testing.T) {
	valid := []string{
		"#submit",
		"div.flex.justify-end:nth-of-type(1) > button.el-button",
		`button.hover\:bg-blue-500`,
	}
	for _, selector := range valid {
		if err := validateBrowserSelector(selector); err != nil {
			t.Fatalf("validateBrowserSelector(%q): %v", selector, err)
		}
	}
	invalid := []string{"", "button{display:none}", "button\nbody"}
	for _, selector := range invalid {
		if err := validateBrowserSelector(selector); err == nil {
			t.Fatalf("validateBrowserSelector(%q) succeeded", selector)
		}
	}
}

func TestBrowserRuntimeSmoke(t *testing.T) {
	executable := os.Getenv("REASONIX_CHROMIUM_SMOKE")
	if executable == "" {
		t.Skip("set REASONIX_CHROMIUM_SMOKE to a Chromium executable")
	}
	t.Setenv(browserExecutableEnv, executable)
	t.Setenv("REASONIX_HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/popup" {
			_, _ = w.Write([]byte("<!doctype html><title>Popup Target</title><main>popup</main>"))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><title>Reasonix Browser Smoke</title><div class="bg-white rounded-lg px-4 py-5 mb-3" style="padding:20px"><button class="el-button hover:bg-blue-500">ready</button></div><section><button class="el-button hover:bg-blue-500">other</button></section><a id="popup" target="_blank" href="/popup">popup</a><a id="restricted" target="_blank" href="chrome://version">restricted</a>`))
	}))
	defer server.Close()

	app := NewApp()
	app.ctx = context.Background()
	app.tabs["tab-browser-smoke"] = &WorkspaceTab{
		ID:            "tab-browser-smoke",
		Scope:         "project",
		WorkspaceRoot: t.TempDir(),
		disabledMCP:   map[string]ServerView{},
	}
	app.tabOrder = []string{"tab-browser-smoke"}
	app.activeTabID = "tab-browser-smoke"
	defer app.browsers.shutdown()

	frames := make(chan BrowserFramePayload, 2)
	exits := make(chan browserExitPayload, 2)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if len(payload) == 0 {
			return
		}
		switch name {
		case browserFrameEvent:
			frame, ok := payload[0].(BrowserFramePayload)
			if ok {
				select {
				case frames <- frame:
				default:
				}
			}
		case browserExitEvent:
			exit, ok := payload[0].(browserExitPayload)
			if ok {
				select {
				case exits <- exit:
				default:
				}
			}
		}
	}

	views := make([]BrowserSessionView, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var opens sync.WaitGroup
	for index := range views {
		opens.Add(1)
		go func(index int) {
			defer opens.Done()
			<-start
			views[index], errs[index] = app.BrowserOpen("tab-browser-smoke", server.URL, 960, 640)
		}(index)
	}
	close(start)
	opens.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("BrowserOpen[%d]: %v", index, err)
		}
	}
	view := views[0]
	if views[1].PageID != view.PageID {
		t.Fatalf("concurrent BrowserOpen created different pages: %+v and %+v", view, views[1])
	}
	if view.PageID == "" || !strings.HasPrefix(view.URL, server.URL) {
		t.Fatalf("BrowserOpen view = %+v", view)
	}
	if err := app.BrowserStartScreencast(view.TabID, view.PageID); err != nil {
		t.Fatalf("BrowserStartScreencast: %v", err)
	}
	hovered, err := app.BrowserInspectorHover(view.TabID, view.PageID, 40, 40)
	if err != nil {
		t.Fatalf("BrowserInspectorHover: %v", err)
	}
	if hovered.Selector == "" || hovered.Tag == "" || hovered.ComputedStyles["font-size"] == "" {
		t.Fatalf("BrowserInspectorHover = %+v", hovered)
	}
	selection, err := app.BrowserInspectorSelect(view.TabID, view.PageID, 40, 40)
	if err != nil {
		t.Fatalf("BrowserInspectorSelect: %v", err)
	}
	if selection.Selector == "" || selection.Tag == "" || !strings.Contains(selection.Selector, " > ") || !strings.Contains(selection.Selector, `hover\:`) {
		t.Fatalf("BrowserInspectorSelect did not produce the expected escaped descendant selector: %+v", selection)
	}
	runtimeSession, err := app.browsers.session(view.TabID, view.PageID)
	if err != nil {
		t.Fatal(err)
	}
drainFrames:
	for {
		select {
		case frame := <-frames:
			if err := app.BrowserFrameAck(frame.TabID, frame.PageID, frame.Sequence); err != nil {
				t.Fatalf("BrowserFrameAck before style update: %v", err)
			}
		default:
			break drainFrames
		}
	}
	runtimeSession.frameMu.Lock()
	frameSequenceBeforeStyle := runtimeSession.frameSequence
	runtimeSession.frameMu.Unlock()
	originalWidth := selection.Box.Width
	originalHeight := selection.Box.Height
	originalComputedWidth := selection.ComputedStyles["width"]
	originalComputedHeight := selection.ComputedStyles["height"]
	styled, err := app.BrowserApplyStyles(view.TabID, view.PageID, map[string]string{
		"color":  "rgb(217, 119, 87)",
		"width":  "240px",
		"height": "80px",
	})
	if err != nil {
		t.Fatalf("BrowserApplyStyles: %v", err)
	}
	if styled.StyleOverrides["color"] == "" || styled.ComputedStyles["width"] != "240px" || styled.ComputedStyles["height"] != "80px" {
		t.Fatalf("BrowserApplyStyles did not refresh computed styles: %+v", styled)
	}
	if styled.Box.Width <= originalWidth || styled.Box.Height <= originalHeight {
		t.Fatalf("BrowserApplyStyles did not refresh element bounds: before=%+v after=%+v", selection.Box, styled.Box)
	}
	if styled.OriginalStyles["width"] != originalComputedWidth || styled.OriginalStyles["height"] != originalComputedHeight {
		t.Fatalf("BrowserApplyStyles replaced original style baseline: before=%+v after=%+v", selection.ComputedStyles, styled.OriginalStyles)
	}
	styleFrameDeadline := time.After(10 * time.Second)
	for {
		select {
		case frame := <-frames:
			if err := app.BrowserFrameAck(frame.TabID, frame.PageID, frame.Sequence); err != nil {
				t.Fatalf("BrowserFrameAck after style update: %v", err)
			}
			if frame.Sequence <= frameSequenceBeforeStyle {
				continue
			}
			if frame.TabID != view.TabID || frame.PageID != view.PageID || frame.Data == "" {
				t.Fatalf("invalid browser frame after style update: %+v", frame)
			}
			goto styleFrameReceived
		case <-styleFrameDeadline:
			t.Fatal("timed out waiting for screencast frame after style update")
		}
	}

styleFrameReceived:
	if err := app.BrowserInspectorClear(view.TabID, view.PageID); err != nil {
		t.Fatalf("BrowserInspectorClear: %v", err)
	}

	permissionSession, err := app.browsers.session(view.TabID, view.PageID)
	if err != nil {
		t.Fatal(err)
	}
	permissionSession.stateMu.Lock()
	permissionSession.state.URL = "https://example.com"
	permissionSession.state.CanAnnotate = false
	permissionSession.stateMu.Unlock()
	if _, err := app.BrowserInspectorHover(view.TabID, view.PageID, 20, 20); err == nil {
		t.Fatal("BrowserInspectorHover allowed an external page")
	}
	if _, err := app.BrowserInspectorSelect(view.TabID, view.PageID, 20, 20); err == nil {
		t.Fatal("BrowserInspectorSelect allowed an external page")
	}
	if _, err := app.BrowserApplyStyles(view.TabID, view.PageID, map[string]string{"color": "red"}); err == nil {
		t.Fatal("BrowserApplyStyles allowed an external page")
	}
	permissionSession.stateMu.Lock()
	permissionSession.state.URL = server.URL
	permissionSession.state.CanAnnotate = true
	permissionSession.stateMu.Unlock()

	resized, err := app.BrowserResize(view.TabID, view.PageID, 800, 500)
	if err != nil {
		t.Fatalf("BrowserResize: %v", err)
	}
	if resized.Width != 800 || resized.Height != 500 {
		t.Fatalf("BrowserResize = %dx%d", resized.Width, resized.Height)
	}

	session, err := app.browsers.session(view.TabID, view.PageID)
	if err != nil {
		t.Fatal(err)
	}
	popupURL := server.URL + "/popup"
	if err := session.runCommand(chromedp.Click("#popup", chromedp.ByID).Do); err != nil {
		t.Fatalf("open popup: %v", err)
	}
	if !waitForBrowserCondition(10*time.Second, func() bool {
		return strings.HasPrefix(session.view().URL, popupURL)
	}) {
		t.Fatalf("popup did not navigate the owning session: %+v", session.view())
	}
	if _, err := session.navigate(server.URL); err != nil {
		t.Fatalf("restore main page: %v", err)
	}
	if err := session.runCommand(chromedp.Click("#restricted", chromedp.ByID).Do); err != nil {
		t.Fatalf("open restricted popup: %v", err)
	}
	time.Sleep(2 * time.Second)
	if !strings.HasPrefix(session.view().URL, server.URL) {
		t.Fatalf("restricted popup changed the owning session: %+v", session.view())
	}

	app.browsers.mu.Lock()
	process := app.browsers.browser.Process()
	app.browsers.mu.Unlock()
	if process == nil {
		t.Fatal("Chromium process is unavailable")
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("kill Chromium: %v", err)
	}
	select {
	case exit := <-exits:
		if exit.Expected || exit.Error == "" || exit.PageID != view.PageID {
			t.Fatalf("unexpected browser crash event: %+v", exit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for browser crash event")
	}
	restarted, err := app.BrowserOpen(view.TabID, server.URL, 800, 500)
	if err != nil {
		t.Fatalf("BrowserOpen after crash: %v", err)
	}
	if restarted.PageID == "" || restarted.PageID == view.PageID {
		t.Fatalf("browser did not create a fresh page after crash: %+v", restarted)
	}
}

func waitForBrowserCondition(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return condition()
}
