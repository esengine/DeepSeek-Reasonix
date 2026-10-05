package sandbox

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// With egress confined, only the proxy port and loopback stay reachable; a
// loopback port closed for a host proxy and every external address do not.
func TestEgressProfileConfinesToTheProxy(t *testing.T) {
	if !Available() {
		t.Skip("sandbox-exec not available")
	}
	_, proxyPort := listenLoopback(t)
	_, closedPort := listenLoopback(t)
	_, devPort := listenLoopback(t)
	sockDir, err := os.MkdirTemp("/tmp", "sbeg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := sockDir + "/s"
	uln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = uln.Close() })
	go func() {
		for {
			c, err := uln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	spec := Spec{Mode: "enforce", Network: true, Egress: fixedRoute{port: proxyPort}, ClosedLoopbackPorts: []int{closedPort}}
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
		return cmd.Run() == nil
	}
	loop := func(port int) string { return "tcp 127.0.0.1:" + strconv.Itoa(port) }
	for what, want := range map[string]bool{
		loop(proxyPort):    true,
		loop(devPort):      true,
		loop(closedPort):   false,
		"self 127.0.0.1:0": true,
		"self [::1]:0":     true,
		"unix " + sock:     true,
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
		t.Error("a confined command reached an external address directly")
	}
}

func TestEgressRulesOnlyWhenNetworkIsOn(t *testing.T) {
	if p := seatbeltProfile(Spec{Network: false, Egress: fixedRoute{port: 4000}}); strings.Contains(p, "localhost:4000") {
		t.Fatalf("network off still opened the proxy port:\n%s", p)
	}
	if p := seatbeltProfile(Spec{Network: true}); strings.Contains(p, "(deny network*)") {
		t.Fatalf("open network without a proxy denied all network:\n%s", p)
	}
}
