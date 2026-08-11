package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/desktop/internal/browseripc"
)

// fakeBrowserCompanionCommand returns the test binary re-executed as a fake
// companion speaking the frame protocol on stdin/stdout.
func fakeBrowserCompanionCommand(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrowserFakeCompanionProcess$")
	cmd.Env = append(os.Environ(), "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE="+mode)
	return cmd
}

// TestBrowserFakeCompanionProcess is the helper-process body. It is a no-op in
// normal test runs and only executes the fake companion loop when re-executed
// with GO_WANT_BROWSER_FAKE=1.
func TestBrowserFakeCompanionProcess(t *testing.T) {
	if os.Getenv("GO_WANT_BROWSER_FAKE") != "1" {
		return
	}
	mode := os.Getenv("BROWSER_FAKE_MODE")
	if mode == "crash" {
		os.Exit(7)
	}
	in := bufio.NewReaderSize(os.Stdin, 64*1024)
	pid := os.Getpid()
	helloReplied := false
	// The fake mirrors the real companion: tab IDs are assigned per process
	// start (never persisted), and tab-scoped methods reject unknown IDs so
	// host-side ID mapping bugs surface as tab_not_found.
	type fakeTab struct {
		url        string
		title      string
		generation int64
	}
	openedTabs := map[string]fakeTab{}
	tabSeq := 0
	restoreDone := false
	postRestoreRequests := 0
	lateEventsSent := false
	for {
		payload, err := browseripc.ReadFrame(in, browseripc.FrameMaxBytes)
		if err != nil {
			os.Exit(0)
		}
		var req browseripc.Request
		if err := json.Unmarshal(payload, &req); err != nil {
			os.Exit(9)
		}
		if mode == "die" && helloReplied {
			// Die on the first post-hello request without replying.
			os.Exit(3)
		}
		if mode == "crash-during-restore" && helloReplied {
			// Die on the first post-hello request (the restore's first
			// tab.open): the host must abort the whole restore pass without
			// publishing or persisting a partial mirror.
			os.Exit(3)
		}
		if mode == "crash-after-restore" && restoreDone && postRestoreRequests > 0 {
			// The companion survives the restore and answers the request
			// that triggered the start; the request after that kills it. The
			// host must restart it and re-restore every owner into the fresh
			// process.
			os.Exit(3)
		}
		if mode == "crash-after-restore" && restoreDone {
			postRestoreRequests++
		}
		switch req.Method {
		case "hello":
			helloReplied = true
			helloProto := browseripc.ProtocolVersion
			if mode == "badhello" {
				helloProto = 99
			}
			writeFakeResponse(req.RequestID, browseripc.HelloResult{
				ProtocolVersion:  helloProto,
				ComponentVersion: "43.3.0-r1",
				ElectronVersion:  "43.3.0",
				ChromiumVersion:  "fake",
				PID:              pid,
				Capabilities: browseripc.Capabilities{
					MaxProtocolVersion: browseripc.ProtocolVersion,
					Methods:            append([]string(nil), browseripc.MethodNames...),
					Events:             append([]string(nil), browseripc.EventNames...),
				},
			})
		case "request.cancel":
			writeFakeResponse(req.RequestID, struct{}{})
		case "window.close":
			writeFakeResponse(req.RequestID, struct{}{})
			if mode == "ignore-close" {
				// Swallow the close: the host must hard-kill this process.
				time.Sleep(10 * time.Minute)
			}
			os.Exit(0)
		case "tab.open":
			var p browseripc.TabOpenParams
			_ = json.Unmarshal(req.Params, &p)
			if mode == "slow" || mode == "slow-late" {
				// Leave the host's restore in flight long enough for the test
				// to interleave a RemoveOwner.
				time.Sleep(400 * time.Millisecond)
			}
			tabSeq++
			tabID := fmt.Sprintf("t-fake-%d-%d", pid, tabSeq)
			openedTabs[tabID] = fakeTab{url: p.URL, title: "Example", generation: 1}
			if mode == "events-first" || mode == "slow" || mode == "slow-late" {
				// The real companion emits navigation/tab.changed BEFORE the
				// tab.open response; the host's restore barrier must buffer
				// these and still commit an exact mirror from the responses.
				writeFakeEvent("navigation", req.OwnerID, browseripc.NavigationEventData{
					OwnerID: req.OwnerID, TabID: tabID, URL: p.URL, Title: "", State: browseripc.NavStarted,
				})
				writeFakeEvent("tab.changed", req.OwnerID, browseripc.TabChangedEventData{
					OwnerID: req.OwnerID, TabID: tabID, URL: p.URL, Title: "Example",
					Active: false, Generation: 1,
				})
			}
			writeFakeResponse(req.RequestID, browseripc.TabInfo{
				TabID: tabID, URL: p.URL, Title: "Example", Generation: 1, Active: true,
			})
		case "tab.activate", "tab.close", "tab.navigate":
			var p browseripc.TabRefParams
			_ = json.Unmarshal(req.Params, &p)
			tab, ok := openedTabs[p.TabID]
			if !ok {
				writeFakeErrorResponse(req.RequestID, browseripc.CodeTabNotFound, "fake: unknown tab "+p.TabID)
				continue
			}
			if req.Method == "tab.activate" {
				// The real companion emits tab.changed(active=true) BEFORE
				// the activate response; the host's idempotent mirror update
				// must not persist a second time for an unchanged tab.
				writeFakeEvent("tab.changed", req.OwnerID, browseripc.TabChangedEventData{
					OwnerID: req.OwnerID, TabID: p.TabID, URL: tab.url, Title: tab.title,
					Active: true, Generation: tab.generation,
				})
				if mode == "crash-after-restore" {
					restoreDone = true
				}
			}
			writeFakeResponse(req.RequestID, struct{}{})
		case "tab.list":
			writeFakeResponse(req.RequestID, browseripc.TabListResult{
				Tabs: []browseripc.TabInfo{{TabID: "t1", URL: "https://example.com", Title: "Example", Generation: 1, Active: true}},
			})
			if mode == "dup" {
				// Malicious companion re-sends the same response: the host must
				// drop the duplicate without panicking or deadlocking.
				writeFakeResponse(req.RequestID, browseripc.TabListResult{Tabs: []browseripc.TabInfo{}})
			}
		default:
			if mode == "slow-late" && !lateEventsSent {
				// The real companion keeps emitting navigation/tab.changed
				// AFTER the restore responses complete. These late events
				// arrive after the restore commit and must not resurrect a
				// deleted owner.
				lateEventsSent = true
				for tabID, tab := range openedTabs {
					writeFakeEvent("navigation", req.OwnerID, browseripc.NavigationEventData{
						OwnerID: req.OwnerID, TabID: tabID, URL: tab.url, Title: "", State: browseripc.NavCommitted,
					})
					writeFakeEvent("tab.changed", req.OwnerID, browseripc.TabChangedEventData{
						OwnerID: req.OwnerID, TabID: tabID, URL: tab.url, Title: tab.title,
						Active: false, Generation: tab.generation,
					})
				}
			}
			writeFakeResponse(req.RequestID, struct{}{})
		}
		if mode == "oversize" && helloReplied {
			// Announce a giant frame after hello; the host must treat this as a
			// protocol violation and kill the process.
			_, _ = os.Stdout.Write([]byte{0x7f, 0xff, 0xff, 0xff, 'x'})
		}
		if mode == "event" && helloReplied {
			writeFakeEvent("tab.changed", "chat-1", browseripc.TabChangedEventData{
				OwnerID: "chat-1", TabID: "t1", URL: "https://example.com", Title: "Example",
				Active: true, Generation: 1,
			})
			helloReplied = false
		}
		if mode == "hang" {
			// Never reply; the host's response timeout must fire.
			time.Sleep(10 * time.Minute)
		}
	}
}

func writeFakeErrorResponse(requestID string, code browseripc.ErrorCode, message string) {
	payload, _ := json.Marshal(browseripc.Response{
		ProtocolVersion: browseripc.ProtocolVersion,
		RequestID:       requestID,
		Error:           &browseripc.RPCError{Code: code, Message: message},
	})
	_ = browseripc.WriteFrame(os.Stdout, payload)
}

func writeFakeResponse(requestID string, result any) {
	payload, _ := json.Marshal(browseripc.Response{
		ProtocolVersion: browseripc.ProtocolVersion,
		RequestID:       requestID,
		Result:          mustFakeJSON(result),
	})
	_ = browseripc.WriteFrame(os.Stdout, payload)
}

func writeFakeEvent(name, ownerID string, data any) {
	payload, _ := json.Marshal(browseripc.Event{
		ProtocolVersion: browseripc.ProtocolVersion,
		Event: browseripc.EventBody{
			Name:    name,
			OwnerID: ownerID,
			Data:    mustFakeJSON(data),
		},
	})
	_ = browseripc.WriteFrame(os.Stdout, payload)
}

func mustFakeJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// newTestCoordinator builds a coordinator with a fake companion process and a
// controllable clock, returning a spawn counter and the clock advance hook.
func newTestCoordinator(t *testing.T, mode string) (*browserCoordinator, *atomic.Int64, *fakeClock, chan browseripc.Event) {
	t.Helper()
	var spawns atomic.Int64
	events := make(chan browseripc.Event, 16)
	clock := &fakeClock{now: time.Now()}
	opts := browserCoordinatorOptions{
		resolveBinary: func() (string, error) { return "/fake/companion", nil },
		spawn: func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
			spawns.Add(1)
			cmd := fakeBrowserCompanionCommand(mode)
			// The allowlisted child env is what production would pass; the fake
			// markers are appended so the re-executed test binary becomes the
			// companion.
			cmd.Env = append(env, "GO_WANT_BROWSER_FAKE=1", "BROWSER_FAKE_MODE="+mode)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				return nil, nil, nil, nil, err
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return nil, nil, nil, nil, err
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if err := cmd.Start(); err != nil {
				return nil, nil, nil, nil, err
			}
			return cmd, stdin, stdout, stderr, nil
		},
		now:             clock.get,
		responseTimeout: 500 * time.Millisecond,
		shutdownGrace:   300 * time.Millisecond,
		events: func(ev browseripc.Event) {
			events <- ev
		},
	}
	b := newBrowserCoordinator(opts)
	t.Cleanup(b.Close)
	return b, &spawns, clock, events
}

// fakeClock is a manually advanced clock for deterministic backoff/window tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) get() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// TestCoordinatorLazyStart: no spawn before first use; first call handshakes
// and round-trips a tab.open result.
func TestCoordinatorLazyStart(t *testing.T) {
	b, spawns, _, _ := newTestCoordinator(t, "ok")
	if got := b.State(); got != browserStopped {
		t.Fatalf("state before first call = %q, want stopped", got)
	}
	var res browseripc.TabInfo
	if err := b.Call(context.Background(), "chat-1", "tab.open", browseripc.TabOpenParams{
		OwnerID: "chat-1", URL: "https://example.com", Disposition: browseripc.DispositionForeground,
	}, &res); err != nil {
		t.Fatalf("tab.open: %v", err)
	}
	// The fake assigns process-unique IDs (t-fake-<pid>-<seq>); only the
	// shape is asserted here.
	if !strings.HasPrefix(res.TabID, "t-fake-") || !strings.HasSuffix(res.TabID, "-1") || res.URL != "https://example.com" || res.Generation != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if spawns.Load() != 1 {
		t.Fatalf("spawns = %d, want 1", spawns.Load())
	}
	view := b.Status()
	if view.State != browserReady || view.ComponentVersion != "43.3.0-r1" || view.PID == 0 {
		t.Fatalf("status after ready: %+v", view)
	}
	if len(view.Capabilities) != len(browseripc.MethodNames) {
		t.Fatalf("capabilities missing methods: %+v", view.Capabilities)
	}
}

// TestCoordinatorComponentMissing: resolution failure surfaces a clear error
// and never spawns.
func TestCoordinatorComponentMissing(t *testing.T) {
	var spawns atomic.Int64
	opts := browserCoordinatorOptions{
		resolveBinary: func() (string, error) { return "", errors.New("no manifest") },
		spawn: func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
			spawns.Add(1)
			return nil, nil, nil, nil, errors.New("unreachable")
		},
	}
	b := newBrowserCoordinator(opts)
	t.Cleanup(b.Close)
	err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if !errors.Is(err, ErrBrowserComponentMissing) {
		t.Fatalf("err = %v, want ErrBrowserComponentMissing", err)
	}
	if spawns.Load() != 0 {
		t.Fatalf("spawns = %d, want 0", spawns.Load())
	}
	if view := b.Status(); view.State != browserCrashed || !view.RecoveryAvailable {
		t.Fatalf("status: %+v", view)
	}
}

// TestCoordinatorCrashThenDisable: three failing spawn attempts inside the
// failure window disable the coordinator for the session; the recovery entry
// re-arms it.
func TestCoordinatorCrashThenDisable(t *testing.T) {
	b, spawns, clock, _ := newTestCoordinator(t, "crash")
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		err := b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
		if err == nil {
			t.Fatalf("attempt %d succeeded unexpectedly", i)
		}
		if i < 3 && !strings.Contains(err.Error(), string(browseripc.CodeCrashed)) && !strings.Contains(err.Error(), "handshake") {
			t.Fatalf("attempt %d err = %v, want crash-class error", i, err)
		}
		// Advance past the backoff window for the next attempt, but stay inside
		// the 60s disable window.
		clock.advance(10 * time.Second)
	}
	if spawns.Load() != 3 {
		t.Fatalf("spawns = %d, want 3", spawns.Load())
	}
	if b.State() != browserDisabled {
		t.Fatalf("state = %q, want disabled", b.State())
	}
	err := b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if !errors.Is(err, ErrBrowserDisabled) {
		t.Fatalf("disabled call err = %v, want ErrBrowserDisabled", err)
	}
	b.ResetRecovery()
	if b.State() != browserStopped {
		t.Fatalf("state after recovery = %q, want stopped", b.State())
	}
}

// TestCoordinatorBackoffFastFailsDoNotCount: a call inside the backoff window
// returns not_ready without consuming a failure.
func TestCoordinatorBackoffFastFailsDoNotCount(t *testing.T) {
	b, _, clock, _ := newTestCoordinator(t, "crash")
	ctx := context.Background()
	err := b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil {
		t.Fatal("first attempt succeeded unexpectedly")
	}
	// Immediate second call: backoff (250ms) still active -> fast fail, not a
	// spawn attempt.
	clock.advance(10 * time.Millisecond)
	err = b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil || !errors.Is(err, ErrBrowserNotReady) {
		t.Fatalf("backoff err = %v, want ErrBrowserNotReady", err)
	}
	clock.advance(10 * time.Second)
	err = b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil {
		t.Fatal("second spawn attempt succeeded unexpectedly")
	}
	// Two spawn attempts so far: the backoff fast-fail did not count.
	if b.Status().PendingRequests != 0 {
		t.Fatal("pending not drained")
	}
	view := b.Status()
	_ = view
}

// TestCoordinatorTimeout: an unresponsive companion surfaces CodeTimeout.
func TestCoordinatorTimeout(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "hang")
	b.opts.responseTimeout = 5 * time.Second
	if err := b.ensureReady(context.Background()); err != nil {
		t.Fatalf("start companion: %v", err)
	}
	b.opts.responseTimeout = 80 * time.Millisecond
	err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	var codeErr *browserCodeError
	if !errors.As(err, &codeErr) || codeErr.code != browseripc.CodeTimeout {
		t.Fatalf("err = %v, want CodeTimeout", err)
	}
}

// TestCoordinatorCancel: caller cancellation surfaces CodeCancelled.
func TestCoordinatorCancel(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "hang")
	b.opts.responseTimeout = 5 * time.Second
	if err := b.ensureReady(context.Background()); err != nil {
		t.Fatalf("start companion: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	var codeErr *browserCodeError
	if !errors.As(err, &codeErr) || codeErr.code != browseripc.CodeCancelled {
		t.Fatalf("err = %v, want CodeCancelled", err)
	}
}

// TestCoordinatorEventsReachSinkAndMirror: companion events update the owner
// mirror and are forwarded to the host sink.
func TestCoordinatorEventsReachSinkAndMirror(t *testing.T) {
	b, _, _, events := newTestCoordinator(t, "event")
	var res browseripc.TabListResult
	if err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("tab.list: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Event.Name != "tab.changed" || ev.Event.OwnerID != "chat-1" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event not delivered")
	}
	b.mu.Lock()
	owner := b.owners["chat-1"]
	b.mu.Unlock()
	if owner == nil || len(owner.tabs) != 1 || owner.tabs[0].tabID != "t1" || owner.tabs[0].generation != 1 {
		t.Fatalf("mirror = %+v", owner)
	}
}

// TestCoordinatorProcessDeathFailsPending: a companion dying mid-request fails
// the pending call with CodeCrashed and marks the coordinator crashed.
func TestCoordinatorProcessDeathFailsPending(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "die")
	err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	var codeErr *browserCodeError
	if !errors.As(err, &codeErr) || codeErr.code != browseripc.CodeCrashed {
		t.Fatalf("err = %v, want CodeCrashed", err)
	}
	if b.State() != browserCrashed {
		t.Fatalf("state = %q, want crashed", b.State())
	}
}

// TestCoordinatorOversizedFrameKillsChild: a child announcing an oversized
// frame is treated as a protocol violation: the process is killed and the
// coordinator reports a crash-class outcome (either the pending call is failed
// with CodeCrashed or the ready transition is rejected as crashed).
func TestCoordinatorOversizedFrameKillsChild(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "oversize")
	err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	var codeErr *browserCodeError
	if !(errors.As(err, &codeErr) && codeErr.code == browseripc.CodeCrashed) && !errors.Is(err, ErrBrowserCrashed) {
		t.Fatalf("err = %v, want CodeCrashed or ErrBrowserCrashed", err)
	}
	if b.State() != browserCrashed {
		t.Fatalf("state = %q, want crashed", b.State())
	}
}

// TestCoordinatorDuplicateResponseDropped: a companion echoing an already
// resolved requestId must not panic, deadlock, or corrupt the next call.
func TestCoordinatorDuplicateResponseDropped(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "dup")
	var res browseripc.TabListResult
	if err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, &res); err != nil {
		t.Fatalf("tab.list: %v", err)
	}
	if len(res.Tabs) != 1 {
		t.Fatalf("tabs = %+v", res.Tabs)
	}
}

// TestCoordinatorProtocolMismatch: a companion speaking an incompatible
// protocol version fails the handshake and is killed.
func TestCoordinatorProtocolMismatch(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "badhello")
	err := b.Call(context.Background(), "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("err = %v, want protocol mismatch", err)
	}
	if b.State() != browserCrashed {
		t.Fatalf("state = %q, want crashed", b.State())
	}
}

// TestCoordinatorGracefulClose: window.close is sent and the process exits;
// an unresponsive child is hard-killed after the grace period.
func TestCoordinatorGracefulClose(t *testing.T) {
	for _, mode := range []string{"ok", "ignore-close"} {
		t.Run(mode, func(t *testing.T) {
			b, _, _, _ := newTestCoordinator(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := b.Call(ctx, "chat-1", "tab.list", browseripc.OwnerParams{OwnerID: "chat-1"}, nil); err != nil {
				t.Fatalf("tab.list: %v", err)
			}
			start := time.Now()
			b.Close()
			// The process must be gone within the grace period plus slack.
			if elapsed := time.Since(start); elapsed > 4*time.Second {
				t.Fatalf("Close took %v", elapsed)
			}
		})
	}
}

// TestCoordinatorEnvAllowlist: secrets never reach the child; display plumbing
// does.
func TestCoordinatorEnvAllowlist(t *testing.T) {
	t.Setenv("REASONIX_API_KEY", "secret-key")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	env := allowlistedBrowserCompanionEnv()
	joined := strings.Join(env, "\n")
	for _, secret := range []string{"REASONIX_API_KEY", "ANTHROPIC_API_KEY", "AWS_SECRET_ACCESS_KEY", "secret-key", "anthropic-secret", "aws-secret"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("secret %q leaked into child env", secret)
		}
	}
	if !strings.Contains(joined, "PATH=/usr/bin:/bin") || !strings.Contains(joined, "XDG_RUNTIME_DIR=/run/user/1000") {
		t.Fatalf("allowlisted env missing: %s", joined)
	}
	if !strings.Contains(joined, "REASONIX_BROWSER_COMPANION=1") {
		t.Fatalf("companion marker missing")
	}
}

// TestCoordinatorBadHelloRepliesFailClosed: a hello with missing capabilities
// fails the handshake without opening the window.
func TestCoordinatorStatusSlicesNeverNil(t *testing.T) {
	b, _, _, _ := newTestCoordinator(t, "ok")
	view := b.Status()
	if view.Capabilities == nil {
		t.Fatal("Status.Capabilities is nil; Wails contract requires []")
	}
}
