package installsource

import (
	"net"
	"testing"
)

func TestInstallGuardJudgesTheIPv4ATransitionAddressCarries(t *testing.T) {
	for _, in := range []string{"64:ff9b::a9fe:a9fe", "2002:a9fe:a9fe::1", "64:ff9b::a00:1"} {
		if !blockedFetchIP(net.ParseIP(in)) {
			t.Errorf("%s reaches an internal address through a transition form but was let through", in)
		}
	}
	if blockedFetchIP(net.ParseIP("64:ff9b::808:808")) {
		t.Error("a transition form carrying a public address must stay reachable")
	}
}
