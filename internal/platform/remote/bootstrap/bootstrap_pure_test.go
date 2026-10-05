package bootstrap

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/sftpfs"
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
	cmd := LaunchCommand(LaunchSpec{Bin: "/usr/bin/reasonix", Workspace: hostile}, paths)

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
	cmd := LaunchCommand(LaunchSpec{Bin: "/usr/bin/reasonix", Workspace: "/ws"}, StatePaths{
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

func TestLocateCommandProbesEveryLaunchFlag(t *testing.T) {
	flags := LaunchFlags(true)
	cmd := LocateCommand("/home/x/.reasonix/remote/bin/reasonix", flags)
	for _, want := range []string{"serve --help", "--version"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("LocateCommand missing %q:\n%s", want, cmd)
		}
	}
	for _, f := range flags {
		for _, want := range []string{"flag " + f + " yes", "flag " + f + " no"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("LocateCommand does not report %q:\n%s", want, cmd)
			}
		}
	}
}

// The probe asks about exactly the flags the launch passes. Nothing else holds
// the two in step, and when they drifted a kernel answered the probe yes on
// --port-file and then exited on the --provider-broker it had never defined —
// which reached the caller as "serve never reported a port".
func TestProbedFlagsAreExactlyTheOnesTheLaunchPasses(t *testing.T) {
	for _, withBroker := range []bool{false, true} {
		spec := LaunchSpec{Bin: "/b/reasonix", Workspace: "/w"}
		paths := StatePaths{Dir: "/d", TokenFile: "/d/t", PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l"}
		if withBroker {
			spec.BrokerAddr = "127.0.0.1:1"
			paths.BrokerEndpoint = "/d/e"
		}
		passed := longFlagsIn(LaunchCommand(spec, paths))
		probed := LaunchFlags(withBroker)
		slices.Sort(passed)
		slices.Sort(probed)
		if !slices.Equal(passed, probed) {
			t.Errorf("withBroker=%v: launch passes %v, probe asks about %v", withBroker, passed, probed)
		}
	}
}

// longFlagsIn reads the --flag names off a launch command line.
func longFlagsIn(cmd string) []string {
	var out []string
	for tok := range strings.FieldsSeq(cmd) {
		if name, ok := strings.CutPrefix(tok, "--"); ok {
			out = append(out, name)
		}
	}
	return out
}

// One place is not every place. A machine can hold an old reasonix on PATH and
// a current one this bootstrap uploaded beside it, and a probe that stopped at
// the first path it found reported only the one that cannot be used.
func TestLocateCommandReportsEveryCandidateItFinds(t *testing.T) {
	cmd := LocateCommand("/home/x/.reasonix/remote/bin/reasonix", LaunchFlags(true))
	for _, want := range []string{"command -v reasonix", "/home/x/.reasonix/remote/bin/reasonix", "npm prefix -g"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("LocateCommand does not look in %q:\n%s", want, cmd)
		}
	}
}

// The probe's records, read back. Order is the order it looked, because that
// is what decides which usable one wins.
func TestParseCandidatesReadsOneRecordPerBinary(t *testing.T) {
	got := parseCandidates("bin /usr/bin/reasonix\nver reasonix 1.31.4\nflag port-file yes\nflag provider-broker no\n" +
		"bin /home/x/.reasonix/remote/bin/reasonix\nver reasonix v2.7.0\nflag port-file yes\nflag provider-broker yes\n" +
		"bin /opt/old/reasonix\nver reasonix dev\nflag port-file no\n")
	want := []candidate{
		{path: "/usr/bin/reasonix", version: "1.31.4", flags: map[string]bool{"port-file": true, "provider-broker": false}},
		{path: "/home/x/.reasonix/remote/bin/reasonix", version: "2.7.0", flags: map[string]bool{"port-file": true, "provider-broker": true}},
		{path: "/opt/old/reasonix", flags: map[string]bool{"port-file": false}},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d candidates, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].path != want[i].path || got[i].version != want[i].version || !maps.Equal(got[i].flags, want[i].flags) {
			t.Errorf("candidate %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A version below the floor is not a usable binary, and a version nothing
// could read is: a source build calls itself "dev", and refusing those would
// take the bootstrap away from everyone developing against it.
func TestUsableTakesTheFloorAndForgivesAnUnreadableVersion(t *testing.T) {
	has := map[string]bool{"port-file": true}
	for _, tc := range []struct {
		name string
		c    candidate
		want bool
	}{
		{"old line", candidate{path: "/usr/bin/reasonix", version: "1.31.4", flags: has}, false},
		{"this line", candidate{path: "/usr/bin/reasonix", version: "2.7.0", flags: has}, true},
		{"the floor itself", candidate{path: "/usr/bin/reasonix", version: "2.0.0", flags: has}, true},
		{"source build", candidate{path: "/usr/bin/reasonix", flags: has}, true},
		{"no port-file flag", candidate{path: "/usr/bin/reasonix", version: "2.7.0"}, false},
	} {
		if got := tc.c.usable(MinPaneVersion, []string{"port-file"}); got != tc.want {
			t.Errorf("%s: usable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A kernel from before the broker owned a pane's model surface still drives a
// pane on its own credentials, and is replaced for one that resolves at home.
func TestPaneFloorRisesOnlyForABrokeredPane(t *testing.T) {
	c := candidate{path: "/usr/bin/reasonix", version: "2.18.0", flags: map[string]bool{}}
	for _, f := range LaunchFlags(true) {
		c.flags[f] = true
	}
	if !c.usable(PaneFloor(false), LaunchFlags(false)) {
		t.Fatal("a 2.18.0 kernel was refused for a pane on its own credentials")
	}
	if c.usable(PaneFloor(true), LaunchFlags(true)) {
		t.Fatal("a 2.18.0 kernel was accepted for a brokered pane")
	}
	c.version = MinBrokeredPaneVersion
	if !c.usable(PaneFloor(true), LaunchFlags(true)) {
		t.Fatal("a kernel at the brokered floor was refused")
	}
}

// probeConn answers the locate probe and nothing else: choosing between the
// binaries a machine holds needs no file layer.
type probeConn struct{ out string }

func (c probeConn) Exec(context.Context, string) (remote.ExecResult, error) {
	return remote.ExecResult{Stdout: []byte(c.out)}, nil
}

func (probeConn) SFTP() (*sftpfs.FS, error) { return nil, errors.New("no file layer here") }

// A machine can hold both, and it usually does after this bootstrap has
// uploaded one: the old install stays on PATH and answers `command -v` first.
// Taking the first path found is how the upload was spent and then ignored.
func TestLocateTakesTheUsableBinaryNotTheFirstOne(t *testing.T) {
	uploaded := "/home/x/.reasonix/remote/bin/reasonix"
	probed := LaunchFlags(false)
	conn := probeConn{out: "bin /usr/bin/reasonix\nver reasonix 1.31.4\n" + allFlagsYes() +
		"bin " + uploaded + "\nver reasonix v2.7.0\n" + allFlagsYes()}
	bin, version := locate(context.Background(), conn, posixShell{}, uploaded, MinPaneVersion, probed)
	if bin != uploaded || version != "2.7.0" {
		t.Fatalf("locate = %q %q, want the uploaded 2.7.0 over the 1.31.4 on PATH", bin, version)
	}
	// Without a floor the caller wants whatever is there, and PATH comes first.
	if bin, _ := locate(context.Background(), conn, posixShell{}, uploaded, "", probed); bin != "/usr/bin/reasonix" {
		t.Fatalf("locate without a floor = %q, want the one on PATH", bin)
	}
}

// "Has none" and "has one from the older line" are different next moves, so
// they are different answers. The version of the newest one turned down is
// what a reader needs to act.
func TestOutdatedNamesTheNewestOneTurnedDown(t *testing.T) {
	found := []candidate{
		{path: "/usr/bin/reasonix", version: "1.29.0"},
		{path: "/opt/reasonix", version: "1.31.4"},
		{path: "/src/reasonix"},
	}
	if got := outdated(found, MinPaneVersion); got != "1.31.4" {
		t.Fatalf("outdated = %q, want 1.31.4", got)
	}
	if got := outdated([]candidate{{path: "/usr/bin/reasonix", version: "2.7.0"}}, MinPaneVersion); got != "" {
		t.Fatalf("outdated = %q, want nothing: this machine has no old kernel", got)
	}
}

// The broker token authenticates a remote kernel to the model credentials at
// home, so it rides a file for the same reason the serve token does: argv is
// readable by every account on the machine, through `ps`. The address rides a
// file too, because a connect publishing the broker elsewhere rewrites it.
func TestLaunchCarriesTheBrokerByFilesNotArgv(t *testing.T) {
	paths := StatePaths{
		Dir: "/d", TokenFile: "/d/t", BrokerEndpoint: "/d/e",
		PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l",
	}
	spec := LaunchSpec{Bin: "/usr/bin/reasonix", Workspace: "/ws", BrokerAddr: "127.0.0.1:41235"}
	cmd := LaunchCommand(spec, paths)
	if strings.Contains(cmd, "41235") {
		t.Fatalf("the broker address reached argv instead of its file:\n%s", cmd)
	}

	for _, want := range []string{"--provider-broker-file '/d/e'"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("launch command is missing %q:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "secret-broker-token") {
		t.Fatal("the broker token reached argv")
	}
}

// A launch with no broker is the ordinary one, and must stay byte-identical:
// the flags a kernel does not understand are the flags an older one refuses.
func TestLaunchWithoutABrokerNamesNoBrokerFlags(t *testing.T) {
	paths := StatePaths{
		Dir: "/d", TokenFile: "/d/t", BrokerTokenFile: "/d/b",
		PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l",
	}
	cmd := LaunchCommand(LaunchSpec{Bin: "/usr/bin/reasonix", Workspace: "/ws"}, paths)
	if strings.Contains(cmd, "provider-broker") {
		t.Fatalf("an unconfigured broker still reached the command line:\n%s", cmd)
	}
}

// A hostile address has no command line to break out of: it only ever
// reaches the file the serve reads, and the serve refuses one off loopback.
func TestLaunchKeepsTheBrokerAddressOffTheCommandLine(t *testing.T) {
	paths := StatePaths{
		Dir: "/d", TokenFile: "/d/t", BrokerEndpoint: "/d/e",
		PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l",
	}
	hostile := "127.0.0.1:1'; rm -rf ~; echo '"
	cmd := LaunchCommand(LaunchSpec{Bin: "/r", Workspace: "/ws", BrokerAddr: hostile}, paths)
	if strings.Contains(cmd, "rm -rf") {
		t.Fatalf("the broker address reached the command line:\n%s", cmd)
	}
}

// A kernel that answers the older half of the launch's flags and not the newer
// is not one this caller can drive. On a real Windows remote such a kernel
// passed the probe on --port-file, was launched with --provider-broker, and
// exited on a flag it had never defined — reported to the reader as "serve
// never reported a port", which sends them to a log the cleanup had removed.
func TestUsableRefusesAKernelMissingTheBrokerFlags(t *testing.T) {
	old := candidate{
		path:    "/home/x/.reasonix/remote/bin/reasonix",
		version: "2.7.0",
		flags: map[string]bool{
			"addr": true, "auth": true, "token-file": true, "port-file": true, "pid-file": true,
		},
	}
	if !old.usable(MinPaneVersion, LaunchFlags(false)) {
		t.Error("a launch passing no broker must accept a kernel that has the rest")
	}
	if old.usable(MinPaneVersion, LaunchFlags(true)) {
		t.Error("a launch that will pass --provider-broker must refuse a kernel without it")
	}
}

// allFlagsYes is what a current kernel answers the probe with: every flag the
// launch might pass, present. Fakes build their locate output from this so a
// flag added to the launch does not quietly leave them describing an old one.
func allFlagsYes() string {
	var b strings.Builder
	for _, f := range LaunchFlags(true) {
		b.WriteString("flag " + f + " yes\n")
	}
	return b.String()
}
