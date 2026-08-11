package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/desktop/internal/browseripc"
)

// testBrowserApp builds an App with the browser surface wired but the
// companion resolution failing, so every call exercises the
// component-missing path without spawning a process.
func testBrowserApp() *App {
	a := &App{
		tabs:        map[string]*WorkspaceTab{"chat-1": {}, "chat-2": {}},
		activeTabID: "chat-1",
		ctx:         context.Background(),
	}
	a.browser = newBrowserCoordinator(browserCoordinatorOptions{
		resolveBinary: func() (string, error) { return "", os.ErrNotExist },
		spawn: func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
			return nil, nil, nil, nil, errors.New("unreachable")
		},
		now: time.Now,
	})
	a.browserState = newBrowserStateStore()
	return a
}

// TestOpenBrowserURLRejectsNonHttp: file/mailto/unknown schemes never reach
// the coordinator; only http(s) opens are accepted.
func TestOpenBrowserURLRejectsNonHttp(t *testing.T) {
	a := testBrowserApp()
	for _, url := range []string{
		"file:///etc/passwd",
		"mailto:test@example.com",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"ftp://example.com",
		"",
	} {
		err := a.OpenBrowserURL("", url, "foreground")
		if err == nil || !strings.Contains(err.Error(), "http(s)") {
			t.Errorf("url %q: err = %v, want http(s) rejection", url, err)
		}
	}
}

// TestOpenBrowserURLTabResolution: empty tabID uses the active chat; unknown
// tabID is rejected before any call.
func TestOpenBrowserURLTabResolution(t *testing.T) {
	a := testBrowserApp()
	err := a.OpenBrowserURL("ghost-chat", "https://example.com", "foreground")
	if err == nil || !strings.Contains(err.Error(), "not open") {
		t.Fatalf("unknown chat: err = %v", err)
	}
	if err := a.OpenBrowserURL("", "https://example.com", "foreground"); err == nil {
		t.Fatal("missing companion should error")
	}
	if err := a.OpenBrowserURL("chat-1", "https://example.com", "sideways"); err == nil ||
		!strings.Contains(err.Error(), "invalid disposition") {
		t.Fatalf("bad disposition: err = %v", err)
	}
}

// TestGetBrowserStatusArrayContract: status surfaces [] never null even in the
// component-missing state, and JSON encodes arrays not null.
func TestGetBrowserStatusArrayContract(t *testing.T) {
	a := testBrowserApp()
	view := a.GetBrowserStatus()
	if view.Capabilities == nil {
		t.Fatal("Capabilities is nil; Wails contract requires []")
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"capabilities":null`) {
		t.Fatalf("Capabilities encodes as null: %s", data)
	}
	if !view.RecoveryAvailable {
		t.Fatal("component-missing state must offer recovery")
	}
}

// TestListBrowserSiteGrantsArrayContract: grants surface is [] never null.
func TestListBrowserSiteGrantsArrayContract(t *testing.T) {
	a := testBrowserApp()
	view, err := a.ListBrowserSiteGrants()
	if err == nil {
		t.Fatal("missing companion should error")
	}
	if view.Grants == nil {
		t.Fatal("Grants is nil; Wails contract requires []")
	}
	data, _ := json.Marshal(view)
	if strings.Contains(string(data), `"grants":null`) {
		t.Fatalf("Grants encodes as null: %s", data)
	}
}

// TestClearBrowserDataValidation: empty and unknown scopes fail cleanly with
// [] returns, never nil.
func TestClearBrowserDataValidation(t *testing.T) {
	a := testBrowserApp()
	cleared, err := a.ClearBrowserData(BrowserDataClearRequest{Scopes: []string{}})
	if err == nil || cleared == nil {
		t.Fatalf("empty scopes: cleared=%v err=%v", cleared, err)
	}
	cleared, err = a.ClearBrowserData(BrowserDataClearRequest{Scopes: []string{"everything"}})
	if err == nil || cleared == nil {
		t.Fatalf("unknown scope: cleared=%v err=%v", cleared, err)
	}
	// Valid scopes forward to the companion and report component_missing.
	cleared, err = a.ClearBrowserData(BrowserDataClearRequest{Scopes: []string{"cookies"}})
	if err == nil || cleared == nil {
		t.Fatalf("valid scope: cleared=%v err=%v", cleared, err)
	}
}

// TestBrowserSettingsRoundTrip: defaults to builtin; patches persist; invalid
// modes are rejected.
func TestBrowserSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := browserSettingsPath
	browserSettingsPath = func() string {
		return dir + "/" + browserSettingsFileName
	}
	t.Cleanup(func() { browserSettingsPath = orig })

	a := testBrowserApp()
	if view := a.GetBrowserSettings(); view.DefaultOpenMode != browserDefaultOpenModeBuiltin {
		t.Fatalf("default mode = %q, want builtin", view.DefaultOpenMode)
	}
	if err := a.UpdateBrowserSettings(BrowserSettingsPatch{DefaultOpenMode: "sideways"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if err := a.UpdateBrowserSettings(BrowserSettingsPatch{DefaultOpenMode: browserDefaultOpenModeSystem}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if view := a.GetBrowserSettings(); view.DefaultOpenMode != browserDefaultOpenModeSystem {
		t.Fatalf("mode after update = %q", view.DefaultOpenMode)
	}
	// Persisted across a fresh App instance.
	if view := testBrowserApp().GetBrowserSettings(); view.DefaultOpenMode != browserDefaultOpenModeSystem {
		t.Fatalf("mode after reload = %q", view.DefaultOpenMode)
	}
}

// TestBrowserSettingsFutureFormatNotOverwritten: settings written by a newer
// format version must survive an older version's update attempt untouched.
func TestBrowserSettingsFutureFormatNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	orig := browserSettingsPath
	browserSettingsPath = func() string {
		return dir + "/" + browserSettingsFileName
	}
	t.Cleanup(func() { browserSettingsPath = orig })

	future := `{"format":"reasonix.browser.settings.v2","version":2,"defaultOpenMode":"system","futureField":"keep-me"}`
	if err := os.WriteFile(browserSettingsPath(), []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	a := testBrowserApp()
	if view := a.GetBrowserSettings(); view.DefaultOpenMode != browserDefaultOpenModeBuiltin {
		t.Fatalf("future settings must fall back to defaults, got %q", view.DefaultOpenMode)
	}
	if err := a.UpdateBrowserSettings(BrowserSettingsPatch{DefaultOpenMode: browserDefaultOpenModeSystem}); err == nil {
		t.Fatal("update over a future-format file must fail")
	}
	after, err := os.ReadFile(browserSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != future {
		t.Fatalf("future settings file was overwritten:\n got: %s\nwant: %s", after, future)
	}
}

// TestInstallBrowserComponentArchive covers the verified-download handoff:
// extraction stays inside the component root and current.json is activated
// only after a complete compatible version directory exists.
func TestInstallBrowserComponentArchive(t *testing.T) {
	home := t.TempDir()
	version := "43.3.0-r1"
	data := browserComponentZIPFixture(t, map[string]string{
		"current.json":                                  `{"version":"` + version + `"}`,
		version + "/component.json":                     `{"format":"reasonix.browser.component.v1","version":"` + version + `","electronVersion":"43.3.0","protocolVersion":1}`,
		version + "/browser/reasonix-browser-companion": "binary",
	})
	if err := installBrowserComponentArchive(data, "component.zip", home, "linux"); err != nil {
		t.Fatalf("installBrowserComponentArchive: %v", err)
	}
	bin := filepath.Join(home, browserComponentDirName, version, "browser", "reasonix-browser-companion")
	if got, err := os.ReadFile(bin); err != nil || string(got) != "binary" {
		t.Fatalf("installed binary = %q, %v", got, err)
	}
	current, err := os.ReadFile(filepath.Join(home, browserComponentDirName, browserCurrentManifest))
	if err != nil || !strings.Contains(string(current), version) {
		t.Fatalf("current manifest = %q, %v", current, err)
	}
}

func TestInstallBrowserComponentArchiveRejectsTraversal(t *testing.T) {
	data := browserComponentZIPFixture(t, map[string]string{"../escape": "owned"})
	if err := installBrowserComponentArchive(data, "component.zip", t.TempDir(), "linux"); err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("traversal error = %v", err)
	}
}

func TestInstallBrowserComponentTarGZ(t *testing.T) {
	home := t.TempDir()
	version := "43.3.0-r1"
	data := browserComponentTarGZFixture(t, map[string]string{
		"current.json":                                  `{"version":"` + version + `"}`,
		version + "/component.json":                     `{"format":"reasonix.browser.component.v1","version":"` + version + `","electronVersion":"43.3.0","protocolVersion":1}`,
		version + "/browser/reasonix-browser-companion": "linux-binary",
	}, nil)
	if err := installBrowserComponentArchive(data, "component.tar.gz", home, "linux"); err != nil {
		t.Fatalf("installBrowserComponentArchive tar.gz: %v", err)
	}
	bin := filepath.Join(home, browserComponentDirName, version, "browser", "reasonix-browser-companion")
	if got, err := os.ReadFile(bin); err != nil || string(got) != "linux-binary" {
		t.Fatalf("installed binary = %q, %v", got, err)
	}
}

func TestInstallBrowserComponentTarGZRejectsEscapingSymlink(t *testing.T) {
	data := browserComponentTarGZFixture(t, nil, map[string]string{"browser-link": "../../escape"})
	if err := installBrowserComponentArchive(data, "component.tar.gz", t.TempDir(), "linux"); err == nil || !strings.Contains(err.Error(), "symlink escapes destination") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestInstallBrowserComponentRejectsVersionSymlinkLeavingVersion(t *testing.T) {
	version := "43.3.0-r1"
	data := browserComponentTarGZFixture(t, map[string]string{
		"current.json":              `{"version":"` + version + `"}`,
		version + "/component.json": `{"format":"reasonix.browser.component.v1","version":"` + version + `","electronVersion":"43.3.0","protocolVersion":1}`,
		"outside-binary":            "not-self-contained",
	}, map[string]string{
		version + "/browser/reasonix-browser-companion": "../../outside-binary",
	})
	err := installBrowserComponentArchive(data, "component.tar.gz", t.TempDir(), "linux")
	if err == nil || !strings.Contains(err.Error(), "leaves the version directory") {
		t.Fatalf("version symlink error = %v", err)
	}
}

func TestExtractBrowserComponentRejectsSymlinkParent(t *testing.T) {
	tests := []struct {
		name    string
		archive func(*testing.T) []byte
		extract func([]byte, string) error
	}{
		{
			name: "zip",
			archive: func(t *testing.T) []byte {
				return browserComponentZIPFixtureWithSymlinks(t,
					map[string]string{"real/payload": "original", "alias/overwrite": "malicious"},
					map[string]string{"alias": "real"},
				)
			},
			extract: extractBrowserComponentZIP,
		},
		{
			name: "tar.gz",
			archive: func(t *testing.T) []byte {
				return browserComponentTarGZFixture(t,
					map[string]string{"real/payload": "original", "alias/overwrite": "malicious"},
					map[string]string{"alias": "real"},
				)
			},
			extract: extractBrowserComponentTarGZ,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.extract(tt.archive(t), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "non-directory parent") {
				t.Fatalf("symlink parent error = %v", err)
			}
		})
	}
}

func TestExtractBrowserComponentRootRejectsPreexistingSymlinkEscape(t *testing.T) {
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "pivot")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	data := browserComponentZIPFixture(t, map[string]string{"pivot/escaped": "owned"})
	if err := extractBrowserComponentZIP(data, dest); err == nil {
		t.Fatal("root-scoped extraction unexpectedly followed a symlink outside the destination")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists after rejected extraction: %v", err)
	}
}

func TestExtractBrowserComponentPreservesInternalSymlink(t *testing.T) {
	dest := t.TempDir()
	data := browserComponentTarGZFixture(t,
		map[string]string{"framework/Versions/A/binary": "electron"},
		map[string]string{"framework/Versions/Current": "A"},
	)
	if err := extractBrowserComponentTarGZ(data, dest); err != nil {
		t.Fatalf("extract internal symlink: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "framework", "Versions", "Current", "binary"))
	if err != nil || string(got) != "electron" {
		t.Fatalf("internal symlink target = %q, %v", got, err)
	}
}

func browserComponentZIPFixture(t *testing.T, files map[string]string) []byte {
	return browserComponentZIPFixtureWithSymlinks(t, files, nil)
}

func browserComponentZIPFixtureWithSymlinks(t *testing.T, files, symlinks map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range symlinks {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(os.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(target)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func browserComponentTarGZFixture(t *testing.T, files, symlinks map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		mode := int64(0o644)
		if filepath.Base(name) == "reasonix-browser-companion" {
			mode = 0o755
		}
		h := &tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range symlinks {
		h := &tar.Header{Name: name, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: target}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestBrowserIPCRequestBudget: a coordinator with a full pending map rejects
// new calls instead of unbounded growth.
func TestBrowserIPCRequestBudget(t *testing.T) {
	a := testBrowserApp()
	// Force the ready state with a sink writer so the call reaches the pending
	// budget check without spawning a companion.
	a.browser.mu.Lock()
	a.browser.state = browserReady
	a.browser.writer = discardWriteCloser{}
	a.browser.mu.Unlock()
	for i := range browseripc.MaxPendingRequests {
		a.browser.mu.Lock()
		a.browser.pending[fmt.Sprintf("req-%d", i)] = &pendingBrowserCall{
			reply: make(chan browseripc.Response, 1),
		}
		a.browser.mu.Unlock()
	}
	err := a.OpenBrowserURL("chat-1", "https://example.com", "foreground")
	if err == nil || !strings.Contains(err.Error(), "limit reached") {
		t.Fatalf("err = %v, want pending limit error", err)
	}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }
