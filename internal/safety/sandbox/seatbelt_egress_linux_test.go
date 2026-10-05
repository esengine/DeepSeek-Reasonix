//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// With egress bridged, the command's namespace reaches the proxy socket through
// its loopback port and nothing else: not the host's loopback, not the network.
func TestBwrapEgressConfinesToTheBridge(t *testing.T) {
	if !Available() || !EgressSupported() {
		t.Skip("bubblewrap egress not usable on this host")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip("no user cache dir")
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(cache, "sbeg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := pongSocket(t, dir)
	port := freeLoopbackPort(t)
	_, hostPort := listenLoopback(t)
	spec := Spec{Mode: "enforce", Network: true, Egress: fixedRoute{port: port, socket: sock}}
	probe := func(sandboxed bool, what string) bool {
		args := []string{os.Args[0], "-test.run=^TestEgressProbeHelper$"}
		if sandboxed {
			var wrapped bool
			if args, wrapped = CommandArgs(spec, args); !wrapped {
				t.Fatal("egress spec was not wrapped")
			}
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = append(os.Environ(), egressProbeEnv+"="+what)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("%s: %v\n%s", what, err, out)
		}
		return err == nil
	}
	for what, want := range map[string]bool{
		"echo 127.0.0.1:" + strconv.Itoa(port):    true,
		"tcp 127.0.0.1:" + strconv.Itoa(hostPort): false,
		"self 127.0.0.1:0":                        true,
	} {
		if got := probe(true, what); got != want {
			t.Errorf("%s reachable = %v, want %v", what, got, want)
		}
	}
	const external = "tcp 1.1.1.1:443"
	if !probe(false, external) {
		t.Log("no external network on this host; skipping the external probe")
		return
	}
	if probe(true, external) {
		t.Error("a bridged command reached an external address directly")
	}
}

// A route with no socket leaves the namespace shut rather than open.
func TestBwrapEgressWithoutASocketKeepsTheNamespace(t *testing.T) {
	args := bwrapArgs(Spec{Mode: "enforce", Network: true, Egress: fixedRoute{port: 3128}}, Shell{Kind: ShellBash, Path: "/bin/bash"}, "true")
	if args[0] != "--unshare-net" {
		t.Fatalf("args = %q, want the network namespace kept", args)
	}
}
