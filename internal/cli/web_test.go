package cli

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/serve"
)

func TestRunDispatchesWebCommand(t *testing.T) {
	isolateCLIConfigHome(t)
	previous := runWebCommand
	t.Cleanup(func() { runWebCommand = previous })

	var got []string
	runWebCommand = func(args []string) int {
		got = append([]string(nil), args...)
		return 29
	}
	if code := Run([]string{"web", "--addr", "127.0.0.1:0"}, "test-version"); code != 29 {
		t.Fatalf("reasonix web exit code = %d, want 29", code)
	}
	if strings.Join(got, " ") != "--addr 127.0.0.1:0" {
		t.Fatalf("reasonix web args = %q", got)
	}
}

func TestHelpListsWebCommand(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := Run([]string{"help"}, "test-version"); rc != 0 {
			t.Fatalf("help rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix web") {
		t.Fatalf("help output missing web command:\n%s", out)
	}
}

func TestWebBrowserURLUsesReachableLoopbackAndAuthEntry(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.ServeConfig
		addr string
		want string
	}{
		{name: "no auth wildcard IPv4", addr: "0.0.0.0:8787", want: "http://127.0.0.1:8787/"},
		{name: "no auth wildcard IPv6", addr: "[::]:8788", want: "http://127.0.0.1:8788/"},
		{name: "password", cfg: config.ServeConfig{AuthMode: "password", PasswordHash: "$2a$12$test"}, addr: "127.0.0.1:8789", want: "http://127.0.0.1:8789/login"},
		{name: "token escaped", cfg: config.ServeConfig{AuthMode: "token", Token: "a token/+"}, addr: "127.0.0.1:8790", want: "http://127.0.0.1:8790/#token=a+token%2F%2B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := control.New(control.Options{SessionDir: t.TempDir()})
			t.Cleanup(ctrl.Close)
			srv := serve.New(ctrl, serve.NewBroadcaster(), tt.cfg)
			if got := webBrowserURL(srv, tt.addr, ""); got != tt.want {
				t.Fatalf("webBrowserURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

type acceptGateListener struct {
	net.Listener
	entered chan struct{}
	release chan struct{}
}

func (l *acceptGateListener) Accept() (net.Conn, error) {
	select {
	case <-l.entered:
	default:
		close(l.entered)
	}
	<-l.release
	return l.Listener.Accept()
}

func TestRunServeListenerOpensOnlyAfterHTTPResponse(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(ctrl.Close)
	srv := serve.New(ctrl, serve.NewBroadcaster(), config.ServeConfig{})
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &acceptGateListener{Listener: raw, entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	opened := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runServeListenerAfterReady(ctx, srv, ln, raw.Addr().String(), func() {
			close(opened)
			cancel()
		})
	}()

	select {
	case <-ln.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP server never reached Accept")
	}
	select {
	case <-opened:
		t.Fatal("browser callback ran before the HTTP server could respond")
	default:
	}
	close(ln.release)
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("browser callback did not run after the HTTP server responded")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServeListenerAfterReady() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after callback canceled its context")
	}
}

func TestRunWebHelpDoesNotOpenBrowser(t *testing.T) {
	previous := openBrowserURL
	t.Cleanup(func() { openBrowserURL = previous })
	openBrowserURL = func(string) error {
		t.Fatal("reasonix web --help must not open a browser")
		return nil
	}
	if code := runWeb([]string{"--help"}); code != 0 {
		t.Fatalf("runWeb(--help) = %d, want 0", code)
	}
}

func TestLaunchWebBrowserInvokesOpenerWithResolvedURL(t *testing.T) {
	previous := openBrowserURL
	t.Cleanup(func() { openBrowserURL = previous })
	var opened string
	openBrowserURL = func(raw string) error {
		opened = raw
		return nil
	}
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(ctrl.Close)
	srv := serve.New(ctrl, serve.NewBroadcaster(), config.ServeConfig{})
	got, err := launchWebBrowser(srv, "0.0.0.0:8787", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:8787/" || opened != got {
		t.Fatalf("launchWebBrowser() = %q, opened %q", got, opened)
	}
}

func TestWebHandoffArgsResumeSameSessionOnDeterministicPort(t *testing.T) {
	got := webHandoffArgs("/tmp/session.jsonl", "session", "deepseek/deepseek-v4-flash", "delivery")
	want := "--resume /tmp/session.jsonl --model deepseek/deepseek-v4-flash --profile delivery"
	if strings.Join(got, " ") != want {
		t.Fatalf("webHandoffArgs() = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestWebBrowserURLUsesSessionDeepLinkBeforeTokenFragment(t *testing.T) {
	ctrl := control.New(control.Options{SessionDir: t.TempDir()})
	t.Cleanup(ctrl.Close)
	srv := serve.New(ctrl, serve.NewBroadcaster(), config.ServeConfig{AuthMode: "token", Token: "secret"})
	got := webBrowserURL(srv, "127.0.0.1:8787", "session with space")
	want := "http://127.0.0.1:8787/sessions/session%20with%20space#token=secret"
	if got != want {
		t.Fatalf("webBrowserURL() = %q, want %q", got, want)
	}
}
