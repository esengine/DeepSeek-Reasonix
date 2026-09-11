package bootstrap

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/remote"
)

func TestLaunchCommandExportsBrowserBrokerEnvOnly(t *testing.T) {
	paths := StatePaths{Dir: "/d", TokenFile: "/d/t", PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l"}
	cmd := LaunchCommand("/usr/bin/reasonix", "/ws", paths, nil, &BrowserBrokerOptions{
		BaseURL: "http://127.0.0.1:41234", Token: "br'oker $tok",
	})
	want := `REASONIX_BROWSER_BROKER='http://127.0.0.1:41234' REASONIX_BROWSER_TOKEN='br'\''oker $tok' $SX nohup '/usr/bin/reasonix' serve`
	if !strings.Contains(cmd, want) {
		t.Fatalf("launch command lacks the quoted broker environment:\n%s", cmd)
	}
	if strings.Contains(cmd, "--browser") {
		t.Fatalf("broker must not travel in argv:\n%s", cmd)
	}
	plain := LaunchCommand("/usr/bin/reasonix", "/ws", paths, nil, nil)
	if strings.Contains(plain, "REASONIX_BROWSER") {
		t.Fatalf("no broker must leave no environment behind:\n%s", plain)
	}
	if !strings.Contains(plain, "SX=setsid; $SX nohup") {
		t.Fatalf("empty prefix changed the launch shape:\n%s", plain)
	}
}

func TestBrowserBrokerSupportedCommandProbesHelpMarker(t *testing.T) {
	cmd := BrowserBrokerSupportedCommand("/home/x/'; rm -rf ~; echo '/reasonix")
	for _, want := range []string{"serve --help", "'browser-broker'", "echo yes", "echo no", `'\''; rm -rf ~; echo '\''`} {
		if !strings.Contains(cmd, want) {
			t.Errorf("probe command missing %q:\n%s", want, cmd)
		}
	}
}

// launchConn scripts a cold start whose located binary answers the browser
// probe with answer; it records the launch command for inspection.
func launchConn(t *testing.T, root, answer string) (*fakeConn, *string) {
	t.Helper()
	var launch string
	paths := pathsFor(root, root)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "grep -q -- 'browser-broker'"):
			return ok(answer + "\n")
		case strings.Contains(cmd, "nohup"):
			launch = cmd
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "ps -p 54321"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	return conn, &launch
}

func TestEnsureServeLaunchesWithBrowserBrokerWhenAdvertised(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn, launch := launchConn(t, root, "yes")
	calls := 0
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", MinVersion: "1.0.0", Clock: time.Now,
		BrowserBroker: func(context.Context) (*BrowserBrokerOptions, error) {
			calls++
			return &BrowserBrokerOptions{BaseURL: "http://127.0.0.1:5151", Token: "gen-1"}, nil
		},
	})
	if err != nil || res.Reused {
		t.Fatalf("EnsureServe: %+v %v", res, err)
	}
	if calls != 1 {
		t.Fatalf("broker callback ran %d times, want once before launch", calls)
	}
	if !strings.Contains(*launch, "REASONIX_BROWSER_BROKER='http://127.0.0.1:5151' REASONIX_BROWSER_TOKEN='gen-1' $SX nohup") {
		t.Fatalf("launch command lacks the broker environment:\n%s", *launch)
	}
}

func TestEnsureServeSkipsBrowserBrokerForOlderServe(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn, launch := launchConn(t, root, "no")
	called := false
	if _, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", MinVersion: "1.0.0", Clock: time.Now,
		BrowserBroker: func(context.Context) (*BrowserBrokerOptions, error) {
			called = true
			return &BrowserBrokerOptions{BaseURL: "http://127.0.0.1:5151", Token: "gen-1"}, nil
		},
	}); err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if called {
		t.Fatal("broker callback must not run (and no forward be opened) for a serve that does not advertise browser-broker")
	}
	if strings.Contains(*launch, "REASONIX_BROWSER") {
		t.Fatalf("older serve received broker environment:\n%s", *launch)
	}
}

func TestEnsureServeLaunchesWithoutBrowserWhenBrokerFails(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn, launch := launchConn(t, root, "yes")
	var steps []string
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", MinVersion: "1.0.0", Clock: time.Now,
		Progress: func(step, detail string) { steps = append(steps, step+":"+detail) },
		BrowserBroker: func(context.Context) (*BrowserBrokerOptions, error) {
			return nil, errors.New("reverse tunnel refused")
		},
	})
	if err != nil || res.State.PID != 54321 {
		t.Fatalf("a failed broker must not block the launch: %+v %v", res, err)
	}
	if strings.Contains(*launch, "REASONIX_BROWSER") {
		t.Fatalf("failed broker leaked environment:\n%s", *launch)
	}
	if !slices.Contains(steps, "browser_broker:unavailable: reverse tunnel refused") {
		t.Fatalf("progress did not report the degraded launch: %v", steps)
	}
}
