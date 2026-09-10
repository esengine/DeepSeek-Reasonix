package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/browser"
	"reasonix/internal/remote/sftpfs"
	"reasonix/internal/remote/sshtest"
)

// brokerFakeExecutor answers from fixed fields and records the session each
// call arrived with (the HTTP handler restores it into the context).
type brokerFakeExecutor struct {
	mu              sync.Mutex
	tabs            []browser.Tab
	screenshot      browser.Screenshot
	downloads       []browser.Download
	sessions        []string
	uploadDirectory string
	onAct           func(browser.ActRequest)
}

func (e *brokerFakeExecutor) note(ctx context.Context) {
	e.mu.Lock()
	e.sessions = append(e.sessions, browser.SessionFromContext(ctx))
	e.mu.Unlock()
}

func (e *brokerFakeExecutor) lastSession() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.sessions) == 0 {
		return ""
	}
	return e.sessions[len(e.sessions)-1]
}

func (e *brokerFakeExecutor) Tabs(ctx context.Context) ([]browser.Tab, error) {
	e.note(ctx)
	return e.tabs, nil
}
func (e *brokerFakeExecutor) Open(ctx context.Context, req browser.OpenRequest) (browser.Tab, error) {
	e.note(ctx)
	return browser.Tab{ID: "tab-new", URL: req.URL}, nil
}
func (e *brokerFakeExecutor) Navigate(ctx context.Context, req browser.NavigateRequest) (browser.Tab, error) {
	e.note(ctx)
	return browser.Tab{ID: req.TabID, URL: req.URL}, nil
}
func (e *brokerFakeExecutor) Snapshot(ctx context.Context, _ browser.SnapshotRequest) (browser.Snapshot, error) {
	e.note(ctx)
	return browser.Snapshot{DocumentToken: "doc-1"}, nil
}
func (e *brokerFakeExecutor) Screenshot(ctx context.Context, _ browser.ScreenshotRequest) (browser.Screenshot, error) {
	e.note(ctx)
	return e.screenshot, nil
}
func (e *brokerFakeExecutor) Act(ctx context.Context, req browser.ActRequest) (browser.ActResult, error) {
	e.note(ctx)
	if e.onAct != nil {
		e.onAct(req)
	}
	return browser.ActResult{Executed: true, Outcome: browser.OutcomeExecuted}, nil
}

func (e *brokerFakeExecutor) captureDir() (string, error) {
	if e.uploadDirectory == "" {
		return "", errors.New("no upload directory")
	}
	return e.uploadDirectory, nil
}
func (e *brokerFakeExecutor) Downloads(ctx context.Context, _ browser.DownloadsRequest) ([]browser.Download, error) {
	e.note(ctx)
	return e.downloads, nil
}
func (e *brokerFakeExecutor) Close(ctx context.Context, _ browser.CloseRequest) error {
	e.note(ctx)
	return nil
}

// brokerTestRig is a running broker with fake resolution, liveness and relay.
type brokerTestRig struct {
	broker  *browserBroker
	baseURL string
	gen     *managedHost
	// current flips liveness; guarded by mu for the -race runs.
	mu      sync.Mutex
	live    bool
	conn    sftpConn
	relays  []relayCall
	relayTo string
}

type relayCall struct {
	workspace string
	localPath string
}

func newBrokerTestRig(t *testing.T, resolve browserSessionResolver) *brokerTestRig {
	t.Helper()
	rig := &brokerTestRig{live: true, gen: &managedHost{}}
	rig.broker = newBrowserBroker(
		resolve,
		func(string, *managedHost) bool { rig.mu.Lock(); defer rig.mu.Unlock(); return rig.live },
		func(string, *managedHost) sftpConn { return rig.conn },
	)
	rig.broker.newRelay = func(sftpConn) FileRelay { return rig }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rig.broker.ln = ln
	rig.broker.port = ln.Addr().(*net.TCPAddr).Port
	server := &http.Server{Handler: rig.broker}
	rig.broker.server = server
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { rig.broker.close() })
	rig.baseURL = fmt.Sprintf("http://127.0.0.1:%d", rig.broker.port)
	return rig
}

// Stage implements FileRelay over the rig, recording the call.
func (r *brokerTestRig) Stage(_ context.Context, workspace, localPath string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relays = append(r.relays, relayCall{workspace: workspace, localPath: localPath})
	if r.relayTo == "" {
		return "", fmt.Errorf("relay unavailable")
	}
	return r.relayTo + filepath.Base(localPath), nil
}

func (r *brokerTestRig) Fetch(_ context.Context, workspace, remotePath, localDirectory string) (string, error) {
	if workspace != "/ws" {
		return "", fmt.Errorf("wrong workspace %s", workspace)
	}
	destination := filepath.Join(localDirectory, filepath.Base(remotePath))
	return destination, os.WriteFile(destination, []byte("remote bytes: "+remotePath), 0o600)
}

func TestBrowserBrokerUploadStagesRemoteBytesAndCleansUp(t *testing.T) {
	stagedPaths := make(chan string, 1)
	exec := &brokerFakeExecutor{uploadDirectory: t.TempDir(), onAct: func(req browser.ActRequest) {
		if len(req.Files) != 1 || req.Files[0] == "/ws/report.csv" {
			t.Errorf("remote path reached local executor: %v", req.Files)
			return
		}
		staged := req.Files[0]
		data, err := os.ReadFile(staged)
		if err != nil || string(data) != "remote bytes: /ws/report.csv" {
			t.Errorf("staged upload: %q %v", data, err)
		}
		stagedPaths <- staged
	}}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	rig.conn = fakeSFTPConn{}
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	client := browser.NewHTTPExecutor(rig.baseURL, token, nil)
	res, err := client.Act(browser.WithSession(context.Background(), "/s"), browser.ActRequest{OperationID: "upload", Action: browser.ActionUpload, Files: []string{"/ws/report.csv"}})
	if err != nil || !res.Executed {
		t.Fatalf("upload: %+v %v", res, err)
	}
	var staged string
	select {
	case staged = <-stagedPaths:
	default:
		t.Fatal("no upload dispatched")
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staging remained after request: %v", err)
	}
}

type browserGatedBody struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (b *browserGatedBody) Read([]byte) (int, error) {
	close(b.entered)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *browserGatedBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

func TestBrowserBrokerRevokesRequestWhileBodyIsPending(t *testing.T) {
	exec := &brokerFakeExecutor{}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	body := &browserGatedBody{entered: make(chan struct{}), closed: make(chan struct{})}
	req := httptest.NewRequest(http.MethodPost, "/v1/browser/act", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(browser.SessionHeader, "/s")
	done := make(chan struct{})
	go func() { defer close(done); rig.broker.ServeHTTP(httptest.NewRecorder(), req) }()
	<-body.entered
	rig.broker.revokeHost("host-1")
	<-done
	if exec.lastSession() != "" {
		t.Fatal("revoked request dispatched")
	}
}

func TestBrowserBrokerRechecksGenerationAfterSessionResolution(t *testing.T) {
	exec := &brokerFakeExecutor{}
	entered, release := make(chan struct{}), make(chan struct{})
	rig := newBrokerTestRig(t, func(string, string) (browserSessionResolution, error) {
		close(entered)
		<-release
		return browserSessionResolution{exec: exec, workspace: "/ws"}, nil
	})
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := browser.NewHTTPExecutor(rig.baseURL, token, nil).Act(browser.WithSession(context.Background(), "/s"), browser.ActRequest{OperationID: "act", Action: browser.ActionClick})
		done <- err
	}()
	<-entered
	rig.setLive(false)
	close(release)
	if err := <-done; !errors.Is(err, browser.ErrNoGrant) {
		t.Fatalf("superseded request: %v", err)
	}
	if exec.lastSession() != "" {
		t.Fatal("superseded request reached executor")
	}
}

func (r *brokerTestRig) setLive(live bool) {
	r.mu.Lock()
	r.live = live
	r.mu.Unlock()
}

func (r *brokerTestRig) relayCalls() []relayCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]relayCall(nil), r.relays...)
}

func brokerTabsCall(t *testing.T, baseURL, token, session string) (int, []browser.Tab) {
	t.Helper()
	exec := browser.NewHTTPExecutor(baseURL, token, nil)
	tabs, err := exec.Tabs(browser.WithSession(context.Background(), session))
	if err != nil {
		return statusOfBrokerError(t, baseURL, token, session), nil
	}
	return http.StatusOK, tabs
}

// statusOfBrokerError re-issues the call raw to read the status code the
// executor mapped into an error.
func statusOfBrokerError(t *testing.T, baseURL, token, session string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/browser/tabs", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.Header.Set(browser.SessionHeader, session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func sessionResolver(exec browser.Executor, workspace string, allowed map[string]bool) browserSessionResolver {
	return func(hostID, sessionPath string) (browserSessionResolution, error) {
		if !allowed[sessionPath] {
			return browserSessionResolution{}, fmt.Errorf("%w: no desktop tab serves session %s", browser.ErrNoGrant, sessionPath)
		}
		return browserSessionResolution{exec: exec, workspace: workspace}, nil
	}
}

func TestBrowserBrokerRoundTripRoutesSession(t *testing.T) {
	exec := &brokerFakeExecutor{tabs: []browser.Tab{{ID: "b1", URL: "https://example.test"}}}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/sessions/a.jsonl": true}))
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	status, tabs := brokerTabsCall(t, rig.baseURL, token, "/sessions/a.jsonl")
	if status != http.StatusOK || len(tabs) != 1 || tabs[0].ID != "b1" {
		t.Fatalf("status=%d tabs=%+v", status, tabs)
	}
	if got := exec.lastSession(); got != "/sessions/a.jsonl" {
		t.Fatalf("executor saw session %q", got)
	}
}

func TestBrowserBrokerTokenRotationRevokesOldGeneration(t *testing.T) {
	exec := &brokerFakeExecutor{}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	oldToken, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	newGen := &managedHost{}
	newToken, _, err := rig.broker.register("host-1", newGen)
	if err != nil {
		t.Fatal(err)
	}
	if oldToken == newToken {
		t.Fatal("token rotation reused the old token")
	}
	if status := statusOfBrokerError(t, rig.baseURL, oldToken, "/s"); status != http.StatusUnauthorized {
		t.Fatalf("old generation token = %d, want 401", status)
	}
	if status := statusOfBrokerError(t, rig.baseURL, newToken, "/s"); status != http.StatusOK {
		t.Fatalf("new generation token = %d, want 200", status)
	}
}

func TestBrowserBrokerRejectsDeadGeneration(t *testing.T) {
	exec := &brokerFakeExecutor{}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	rig.setLive(false)
	if status := statusOfBrokerError(t, rig.baseURL, token, "/s"); status != http.StatusUnauthorized {
		t.Fatalf("dead generation = %d, want 401", status)
	}
	rig.setLive(true)
	if status := statusOfBrokerError(t, rig.baseURL, token, "/s"); status != http.StatusOK {
		t.Fatalf("live generation = %d, want 200", status)
	}
	rig.broker.revokeHost("host-1")
	if status := statusOfBrokerError(t, rig.baseURL, token, "/s"); status != http.StatusUnauthorized {
		t.Fatalf("revoked host = %d, want 401", status)
	}
}

func TestBrowserBrokerRejectsForeignAndMissingSessions(t *testing.T) {
	exec := &brokerFakeExecutor{}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/sessions/a.jsonl": true}))
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"/sessions/other.jsonl", ""} {
		status := statusOfBrokerError(t, rig.baseURL, token, session)
		if status != http.StatusConflict {
			t.Fatalf("session %q = %d, want 409 no_grant", session, status)
		}
	}
	if len(exec.sessions) != 0 {
		t.Fatalf("executor saw %d calls from rejected sessions", len(exec.sessions))
	}
}

func TestBrowserBrokerRejectsBadToken(t *testing.T) {
	rig := newBrokerTestRig(t, sessionResolver(&brokerFakeExecutor{}, "/ws", map[string]bool{"/s": true}))
	if _, _, err := rig.broker.register("host-1", rig.gen); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "wrong"} {
		if status := statusOfBrokerError(t, rig.baseURL, token, "/s"); status != http.StatusUnauthorized {
			t.Fatalf("token %q = %d, want 401", token, status)
		}
	}
	req, err := http.NewRequest(http.MethodGet, rig.baseURL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("healthz = %d, want 204", resp.StatusCode)
	}
}

func TestBrowserBrokerScreenshotRelaysCapture(t *testing.T) {
	exec := &brokerFakeExecutor{screenshot: browser.Screenshot{Path: "/tmp/reasonix-browser/tab-1/shot.png", MIME: "image/png", Width: 10, Height: 10}}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	rig.conn = fakeSFTPConn{}
	rig.relayTo = "/remote/scratch/"
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	httpExec := browser.NewHTTPExecutor(rig.baseURL, token, nil)
	shot, err := httpExec.Screenshot(browser.WithSession(context.Background(), "/s"), browser.ScreenshotRequest{TabID: "tab-1"})
	if err != nil {
		t.Fatal(err)
	}
	if shot.Path != "/remote/scratch/shot.png" {
		t.Fatalf("screenshot path = %q, want the relayed remote path", shot.Path)
	}
	calls := rig.relayCalls()
	if len(calls) != 1 || calls[0].workspace != "/ws" || calls[0].localPath != "/tmp/reasonix-browser/tab-1/shot.png" {
		t.Fatalf("relay calls = %+v", calls)
	}
}

func TestBrowserBrokerScreenshotWithoutConnectionFails(t *testing.T) {
	exec := &brokerFakeExecutor{screenshot: browser.Screenshot{Path: "/tmp/shot.png"}}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	httpExec := browser.NewHTTPExecutor(rig.baseURL, token, nil)
	if _, err := httpExec.Screenshot(browser.WithSession(context.Background(), "/s"), browser.ScreenshotRequest{TabID: "t"}); err == nil {
		t.Fatal("screenshot without a live connection succeeded")
	}
	if calls := rig.relayCalls(); len(calls) != 0 {
		t.Fatalf("relay ran without a connection: %+v", calls)
	}
}

func TestBrowserBrokerDownloadsRelayEachPath(t *testing.T) {
	exec := &brokerFakeExecutor{downloads: []browser.Download{
		{ID: "d1", Path: "/tmp/dl/a.zip"},
		{ID: "d2", Path: ""},
	}}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	rig.conn = fakeSFTPConn{}
	rig.relayTo = "/remote/scratch/"
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	httpExec := browser.NewHTTPExecutor(rig.baseURL, token, nil)
	downloads, err := httpExec.Downloads(browser.WithSession(context.Background(), "/s"), browser.DownloadsRequest{TabID: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 2 || downloads[0].Path != "/remote/scratch/a.zip" || downloads[1].Path != "" {
		t.Fatalf("downloads = %+v", downloads)
	}
	if calls := rig.relayCalls(); len(calls) != 1 {
		t.Fatalf("relay calls = %+v, want exactly the non-empty path", calls)
	}
}

// fakeSFTPConn satisfies sftpConn without a server; Stage never reaches it in
// rig-based tests because newRelay is faked.
type fakeSFTPConn struct{}

func (fakeSFTPConn) SFTP() (*sftpfs.FS, error) { return nil, fmt.Errorf("no sftp") }

func TestBrowserBrokerConcurrentRotation(t *testing.T) {
	exec := &brokerFakeExecutor{tabs: []browser.Tab{{ID: "b1"}}}
	rig := newBrokerTestRig(t, sessionResolver(exec, "/ws", map[string]bool{"/s": true}))
	token, _, err := rig.broker.register("host-1", rig.gen)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				_, _, _ = rig.broker.register("host-1", &managedHost{})
			}
		})
	}
	for range 8 {
		wg.Go(func() {
			for range 20 {
				_, _ = brokerTabsCall(t, rig.baseURL, token, "/s")
			}
		})
	}
	wg.Wait()
	// After the dust settles only the last minted token authenticates.
	if status := statusOfBrokerError(t, rig.baseURL, token, "/s"); status != http.StatusUnauthorized {
		t.Fatalf("superseded token = %d, want 401", status)
	}
}

func TestSFTPFileRelayRoundTrip(t *testing.T) {
	root := t.TempDir()
	server := sshtest.Start(t, sshtest.Options{Password: "pw", SFTPRoot: root})
	cl, err := ssh.Dial("tcp", server.Addr, &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	fs, err := sftpfs.New(cl)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(local, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	remotePath, err := (sftpFileRelay{conn: sftpFSConn{fs: fs}}).Stage(context.Background(), "/work space", local)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("relayed file unreadable at %q: %v", remotePath, err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("relayed content = %q", data)
	}
	if !strings.Contains(remotePath, "browser-relay") {
		t.Fatalf("remote path %q is outside the relay scratch area", remotePath)
	}
	info, err := os.Stat(remotePath)
	if err != nil {
		t.Fatal(err)
	}
	// Windows exposes only the read-only bit through chmod; the Windows
	// round-trip still checks native file access and both transfer directions.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("relayed file mode = %v, want 0600", info.Mode().Perm())
	}
	// The reverse direction must download the remote bytes and enforce the
	// remote workspace boundary before handing a desktop path to Chromium.
	relay := sftpFileRelay{conn: sftpFSConn{fs: fs}}
	staged, err := relay.Fetch(context.Background(), filepath.Dir(remotePath), remotePath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(staged)
	if err != nil || string(data) != "png-bytes" {
		t.Fatalf("remote upload content = %q %v", data, err)
	}
	if _, err := relay.Fetch(context.Background(), filepath.Join(root, "unowned"), local, t.TempDir()); err == nil {
		t.Fatal("foreign remote file was staged")
	}
}

type sftpFSConn struct{ fs *sftpfs.FS }

func (c sftpFSConn) SFTP() (*sftpfs.FS, error) { return c.fs, nil }

func TestSFTPFileRelayRejectsOddFiles(t *testing.T) {
	dir := t.TempDir()
	relay := sftpFileRelay{conn: sftpFSConn{}}
	if _, err := relay.Stage(context.Background(), "/ws", filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("missing file staged")
	}
	if _, err := relay.Stage(context.Background(), "/ws", dir); err == nil {
		t.Fatal("directory staged")
	}
}

func TestRelayFileNameSanitizes(t *testing.T) {
	name := relayFileName("/tmp/x/evil.png")
	if strings.Contains(name, "/") || !strings.HasSuffix(name, "-evil.png") {
		t.Fatalf("relayFileName = %q", name)
	}
	if name == relayFileName("/tmp/x/evil.png") {
		t.Fatal("relayFileName must be unique per call")
	}
}

func TestBrowserRelayRemotePathContract(t *testing.T) {
	for _, tc := range []struct {
		name, home, agentPath, wirePath string
	}{
		{"posix", "/home/user", "/workspace/report.csv", "/workspace/report.csv"},
		{"posix backslash filename", "/home/user", "/workspace/report\\name.csv", "/workspace/report\\name.csv"},
		{"posix drive-like directory", "/home/user", "/C:/report.csv", "/C:/report.csv"},
		{"windows native", "/C:/Users/user", `C:\work space\report.csv`, "/C:/work space/report.csv"},
		{"windows other drive", "/C:/Users/user", `D:\work space\report.csv`, "/D:/work space/report.csv"},
		{"windows forward slashes", "/C:/Users/user", "C:/work space/report.csv", "/C:/work space/report.csv"},
		{"windows canonical", "/C:/Users/user", "/C:/work space/report.csv", "/C:/work space/report.csv"},
		{"windows unprefixed server", "C:/Users/user", `D:\work space\report.csv`, "D:/work space/report.csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := relaySFTPPath(tc.agentPath, tc.home); got != tc.wirePath {
				t.Fatalf("wire path = %q, want %q", got, tc.wirePath)
			}
			if got := relaySFTPPath(relayAgentPath(tc.wirePath, tc.home), tc.home); got != tc.wirePath {
				t.Fatalf("Agent path round trip = %q, want %q", got, tc.wirePath)
			}
		})
	}
	for _, wire := range []string{"/C:/Users/user/capture.png", "D:/work/report.csv"} {
		if got := relayAgentPath(wire, "/C:/Users/user"); !relayWindowsDrivePath(got) || strings.HasPrefix(got, "/") {
			t.Fatalf("Windows Agent cannot consume %q", got)
		}
	}
	if got := relayAgentPath("/C:/report.csv", "/home/user"); got != "/C:/report.csv" {
		t.Fatalf("POSIX directory changed to a Windows drive: %q", got)
	}
	for _, relative := range []string{"C:report.csv", "report.csv", "../report.csv"} {
		if relayWindowsDrivePath(relative) {
			t.Fatalf("relative path accepted as absolute: %q", relative)
		}
	}
}
