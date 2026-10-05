//go:build !windows

package sandbox

import (
	"os"
	"strconv"
	"testing"
)

// The bridge carries a connection on its loopback port to the proxy socket
// and reports the command's exit the way a shell would.
func TestEgressBridgeCarriesTrafficAndExitStatus(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sbbr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := pongSocket(t, dir)
	port := freeLoopbackPort(t)
	t.Setenv(egressProbeEnv, "echo 127.0.0.1:"+strconv.Itoa(port))
	if got := runEgressBridge(port, sock, []string{os.Args[0], "-test.run=^TestEgressProbeHelper$"}); got != 0 {
		t.Fatalf("probe through the bridge exited %d, want 0", got)
	}
	t.Setenv(egressProbeEnv, "")
	for script, want := range map[string]int{"exit 3": 3, "kill -TERM $$": 143} {
		if got := runEgressBridge(freeLoopbackPort(t), sock, []string{"/bin/sh", "-c", script}); got != want {
			t.Errorf("%q exited %d, want %d", script, got, want)
		}
	}
	if got := runEgressBridge(freeLoopbackPort(t), sock, []string{"/nonexistent/cmd"}); got != 127 {
		t.Errorf("a missing command exited %d, want 127", got)
	}
}
