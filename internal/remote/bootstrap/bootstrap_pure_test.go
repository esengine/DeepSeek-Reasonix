package bootstrap

import (
	"strings"
	"testing"
)

func TestParseUname(t *testing.T) {
	cases := []struct {
		in           string
		goos, goarch string
		wantErr      bool
	}{
		{"Linux x86_64", "linux", "amd64", false},
		{"Linux aarch64", "linux", "arm64", false},
		{"Darwin arm64", "darwin", "arm64", false},
		{"Darwin x86_64", "darwin", "amd64", false},
		{"Linux armv7l", "linux", "arm", false},
		{"  Linux   x86_64  \n", "linux", "amd64", false},
		{"MINGW64_NT-10.0 x86_64", "", "", true}, // Windows shell
		{"Linux mips", "", "", true},
		{"garbage", "", "", true},
	}
	for _, c := range cases {
		goos, goarch, err := ParseUname(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseUname(%q): expected error", c.in)
			}
			continue
		}
		if err != nil || goos != c.goos || goarch != c.goarch {
			t.Errorf("ParseUname(%q) = (%q,%q,%v), want (%q,%q)", c.in, goos, goarch, err, c.goos, c.goarch)
		}
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"reasonix v1.9.0":        "1.9.0",
		"1.9.0":                  "1.9.0",
		"reasonix version 2.0.1": "2.0.1",
		"v1.10.0-rc.1":           "1.10.0-rc.1",
	}
	for in, want := range cases {
		got, err := ParseVersion(in)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = (%q,%v), want %q", in, got, err, want)
		}
	}
	if _, err := ParseVersion("no version here"); err == nil {
		t.Error("expected error for versionless output")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.9.0", "1.9.0", 0},
		{"1.9.0", "1.10.0", -1},
		{"1.10.0", "1.9.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.9", "1.9.0", 0},
		{"1.9.1-rc.1", "1.9.1", 0}, // pre-release ignored for ordering
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestLaunchCommandQuotesHostilePaths is the security golden: a workspace or
// log path containing shell metacharacters must be fully single-quoted so it
// cannot break out of the launch command.
func TestLaunchCommandQuotesHostilePaths(t *testing.T) {
	paths := StatePaths{
		Dir:       "/home/dev/.reasonix/remote",
		TokenFile: "/home/dev/.reasonix/remote/serve-x.token",
		PortFile:  "/home/dev/.reasonix/remote/serve-x.port",
		PidFile:   "/home/dev/.reasonix/remote/serve-x.pid",
		LogFile:   "/home/dev/.reasonix/remote/serve-x.log",
	}
	hostile := "/tmp/'; rm -rf ~; echo '"
	cmd := LaunchCommand("/usr/bin/reasonix", hostile, paths)

	// The hostile workspace must appear only inside a quoted operand, escaped.
	if strings.Contains(cmd, "; rm -rf ~; echo") && !strings.Contains(cmd, `'\''; rm -rf ~; echo '\''`) {
		t.Fatalf("hostile workspace not properly escaped:\n%s", cmd)
	}
	// No unescaped `rm -rf` sequence that would execute.
	if strings.Contains(cmd, "cd /tmp/'; rm -rf") {
		t.Fatalf("workspace broke out of quoting:\n%s", cmd)
	}
	// Sanity: the essential flags are present.
	for _, want := range []string{"--addr 127.0.0.1:0", "--auth token", "--token-file", "--port-file", "$SX nohup", "echo $!"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("launch command missing %q:\n%s", want, cmd)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"simple":      "'simple'",
		"has space":   "'has space'",
		"a'b":         `'a'\''b'`,
		"'; rm -rf ~": `''\''; rm -rf ~'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStopAndServeAliveCommands(t *testing.T) {
	paths := StatePaths{TokenFile: "/state/ws.token", PortFile: "/state/ws.port"}
	stop := StopCommand(4321, paths)
	for _, want := range []string{"kill -TERM 4321", "kill -0 4321", "kill -KILL 4321"} {
		if !strings.Contains(stop, want) {
			t.Errorf("StopCommand missing %q: %s", want, stop)
		}
	}
	alive := ServeAliveCommand(99, paths)
	// Must check liveness AND that the process is a reasonix serve (guards PID
	// reuse), not just kill -0.
	for _, want := range []string{"kill -0 99", "ps -p 99", "*reasonix*serve*", paths.TokenFile, paths.PortFile} {
		if !strings.Contains(alive, want) {
			t.Errorf("ServeAliveCommand missing %q: %s", want, alive)
		}
	}
	if strings.Count(stop, "ours") < 3 {
		t.Fatalf("StopCommand must revalidate ownership during TERM/KILL wait: %s", stop)
	}
}

func TestLaunchCommandDetachAndLogHardening(t *testing.T) {
	cmd := LaunchCommand("/usr/bin/reasonix", "/ws", StatePaths{
		Dir: "/d", TokenFile: "/d/t", PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l",
	})
	// setsid must be optional (macOS lacks it) and the log created 0600 so the
	// serve token line (already suppressed under --port-file) can't leak.
	for _, want := range []string{"command -v setsid", "$SX nohup", "chmod 600", "umask 077", "--port-file"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("LaunchCommand missing %q:\n%s", want, cmd)
		}
	}
	if !strings.Contains(cmd, "rm -f '/d/p' '/d/i'") {
		t.Fatalf("LaunchCommand does not clear stale port/pid files before launch:\n%s", cmd)
	}
	if strings.Contains(cmd, "setsid nohup") {
		t.Errorf("setsid must be conditional, not hard-wired:\n%s", cmd)
	}
}

func TestLocateCommandProbesPortFileFlag(t *testing.T) {
	cmd := LocateCommand("/home/x/.reasonix/remote/bin/reasonix")
	for _, want := range []string{"serve --help", "port-file", "portfile:yes", "portfile:no"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("LocateCommand missing %q:\n%s", want, cmd)
		}
	}
}
