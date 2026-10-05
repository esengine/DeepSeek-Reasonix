//go:build !darwin && !windows

package sandbox

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// egressBridgeEnv asks this binary, started inside a bubblewrap network
// namespace, to run as the egress bridge: "<port> <socket>". The namespace has
// no route out, so the bridge is the one way its commands reach the proxy.
const egressBridgeEnv = "REASONIX_EGRESS_BRIDGE"

// The bridge is an entry of whichever host binary imports this package, so no
// separate helper has to be shipped and found. It clears the variable before
// anything else runs, or the command it starts would become a bridge too.
func init() {
	v, ok := os.LookupEnv(egressBridgeEnv)
	if !ok {
		return
	}
	_ = os.Unsetenv(egressBridgeEnv)
	portText, socket, _ := strings.Cut(v, " ")
	port, err := strconv.Atoi(portText)
	if err != nil || socket == "" || len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "reasonix sandbox: malformed egress bridge request")
		os.Exit(126)
	}
	os.Exit(runEgressBridge(port, socket, os.Args[1:]))
}
