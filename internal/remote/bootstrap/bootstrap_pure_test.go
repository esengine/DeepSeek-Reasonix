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
	cmd := LaunchCommand("/usr/bin/reasonix", hostile, paths, nil)

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
	withModel := ServeAliveCommand(99, paths, "--model reasonix-desktop-proxy")
	for _, want := range []string{`R0='--model reasonix-desktop-proxy'`, `"$R0"*`} {
		if !strings.Contains(withModel, want) {
			t.Errorf("ServeAliveCommand(requireArgs) missing %q: %s", want, withModel)
		}
	}
	if strings.Contains(alive, "R0=") {
		t.Errorf("plain ServeAliveCommand must not grow require-arg vars: %s", alive)
	}
}

func TestLaunchCommandDetachAndLogHardening(t *testing.T) {
	cmd := LaunchCommand("/usr/bin/reasonix", "/ws", StatePaths{
		Dir: "/d", TokenFile: "/d/t", PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l",
	}, nil)
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

func TestLocateCommandProbesRequiredServeCapabilities(t *testing.T) {
	cmd := LocateCommand("/home/x/.reasonix/remote/bin/reasonix")
	for _, want := range []string{"serve --help", "port-file", "session-events", "detached-heal", ServeCapsToken, "portfile:yes", "sessionevents:yes", "detachedheal:yes", "caps:yes"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("LocateCommand missing %q:\n%s", want, cmd)
		}
	}
}

func TestLocateUploadedCommandBypassesPathCandidates(t *testing.T) {
	uploaded := "/home/x/.reasonix/remote/bin/reasonix"
	cmd := LocateUploadedCommand(uploaded)
	if !strings.Contains(cmd, "BIN='"+uploaded+"'") || strings.Contains(cmd, "command -v reasonix") || strings.Contains(cmd, "npm prefix") {
		t.Fatalf("uploaded probe did not target only the managed binary:\n%s", cmd)
	}
}

func TestLocateNPMGlobalCommandBypassesPathCandidates(t *testing.T) {
	cmd := LocateNPMGlobalCommand()
	if !strings.Contains(cmd, `P="$(npm prefix -g 2>/dev/null)"`) ||
		!strings.Contains(cmd, `BIN="$P/bin/reasonix"`) ||
		strings.Contains(cmd, "command -v reasonix") {
		t.Fatalf("npm-global probe did not target only npm's installed binary:\n%s", cmd)
	}
}

func TestSupportsRequiredServeCapabilitiesCommand(t *testing.T) {
	cmd := SupportsRequiredServeCapabilitiesCommand(42)
	for _, want := range []string{"/proc/42/exe", "session-events", "detached-heal", ServeCapsToken} {
		if !strings.Contains(cmd, want) {
			t.Errorf("capability probe missing %q:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "ps -p 42") {
		t.Fatalf("capability probe must not execute a replaced pathname reported by ps:\n%s", cmd)
	}
}

func TestTomlAssignmentString(t *testing.T) {
	cases := []struct {
		line  string
		key   string
		value string
		ok    bool
	}{
		{`name = "proxy-1"`, "name", "proxy-1", true},
		{`name="proxy-1"`, "name", "proxy-1", true},
		{`name     =     "proxy-1"`, "name", "proxy-1", true},
		{`name        = "proxy-1"`, "name", "proxy-1", true},
		{`name = "proxy-1"   # aligned by a config normalizer`, "name", "proxy-1", true},
		{`name = "proxy-2"`, "name", "proxy-2", true},
		{`names = "proxy-1"`, "name", "proxy-1", false},
		{`name = proxy-1`, "name", "proxy-1", false},
		{`base_url = "http://127.0.0.1:41333"`, "base_url", "http://127.0.0.1:41333", true},
		{`kind        = "openai"`, "kind", "openai", true},
		{`model       = "deepseek-v4-flash"`, "model", "deepseek-v4-flash", true},
		{`max_output_tokens = 131072`, "model", "131072", false},
	}
	for _, c := range cases {
		got, ok := tomlAssignmentString(c.line, c.key)
		if ok != c.ok || (ok && got != c.value) {
			t.Errorf("tomlAssignmentString(%q, %q) = %q, %v; want %q, %v", c.line, c.key, got, ok, c.value, c.ok)
		}
	}
}

// staleAlignedProxyConfig reproduces a remote config observed in the wild: an
// older heal installed the provider with aligned assignments and no marker
// comment, a later heal failed to find that block (exact-match parser) and
// appended a duplicate, and the loader resolved the name to the stale first
// block — leaving the serve dialing a dead tunnel port.
const staleAlignedProxyConfig = `# Reasonix configuration.
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name        = "reasonix-desktop-proxy-bc965691ed1e10b8"
kind        = "openai"
base_url    = "http://127.0.0.1:46407"
model       = "deepseek-v4-pro"
api_key_env = "REASONIX_PROXY_TOKEN_BC965691ED1E10B8"

[[providers]]
# managed by the Reasonix desktop credential proxy — safe to delete
name = "reasonix-desktop-proxy-bc965691ed1e10b8"
kind = "openai"
base_url = "http://127.0.0.1:41333"
model = "deepseek-v4-pro"
api_key_env = "REASONIX_PROXY_TOKEN_BC965691ED1E10B8"

[[providers]]
name = "mine"
kind = "openai"
base_url = "https://api.deepseek.com"
api_key_env = "MY_KEY"
`

func TestProviderBlockIndexFindsAlignedBlock(t *testing.T) {
	if idx := providerBlockIndex(staleAlignedProxyConfig, "reasonix-desktop-proxy-bc965691ed1e10b8"); idx < 0 {
		t.Fatal("aligned provider block not found")
	}
	if idx := providerBlockIndex(staleAlignedProxyConfig, "mine"); idx < 0 {
		t.Fatal("user provider block not found")
	}
	if idx := providerBlockIndex(staleAlignedProxyConfig, "absent"); idx >= 0 {
		t.Fatalf("absent provider reported at %d", idx)
	}
}

func TestDropDuplicateProviderBlocks(t *testing.T) {
	deduped, changed := dropDuplicateProviderBlocks(staleAlignedProxyConfig, "reasonix-desktop-proxy-bc965691ed1e10b8")
	if !changed {
		t.Fatal("duplicate blocks were not dropped")
	}
	if got := strings.Count(deduped, `"reasonix-desktop-proxy-bc965691ed1e10b8"`); got != 1 {
		t.Fatalf("want exactly one provider block, got %d:\n%s", got, deduped)
	}
	if !strings.Contains(deduped, `name = "mine"`) {
		t.Fatalf("unrelated provider block lost:\n%s", deduped)
	}
	if unchanged, changed := dropDuplicateProviderBlocks(deduped, "reasonix-desktop-proxy-bc965691ed1e10b8"); changed {
		t.Fatalf("second dedup rewrote an already-clean config:\n%s", unchanged)
	}
}

func TestRewriteManagedProviderBaseURLsHealsAlignedBlocks(t *testing.T) {
	rewritten, changed := rewriteManagedProviderBaseURLs(staleAlignedProxyConfig, "http://127.0.0.1:41333")
	if !changed {
		t.Fatal("stale aligned base_url was not rewritten")
	}
	if got := strings.Count(rewritten, `base_url = "http://127.0.0.1:41333"`); got != 2 {
		t.Fatalf("both managed blocks must follow the tunnel port, got %d:\n%s", got, rewritten)
	}
	if !strings.Contains(rewritten, `base_url = "https://api.deepseek.com"`) {
		t.Fatalf("user provider base_url was rewritten:\n%s", rewritten)
	}
}
