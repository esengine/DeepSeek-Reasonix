package sandbox

import "testing"

func TestParseAuthoritiesDropsUnknownNames(t *testing.T) {
	got := ParseAuthorities([]string{" ssh_agent ", "kubernetes", "docker", ""})
	if len(got) != 2 || got[0] != SSHAgent || got[1] != Docker {
		t.Fatalf("ParseAuthorities = %v, want [ssh_agent docker]", got)
	}
	if (Spec{}).Granted(SSHAgent) {
		t.Error("the zero Spec granted an authority; a new call site must be confined by default")
	}
}

// The promise is "these endpoints are governed", not "host IPC is isolated".
// If this list grows, the wording that ships with it has to grow too.
func TestGovernedSetIsNarrowerThanHostIPC(t *testing.T) {
	if got := GovernedAuthorities(); len(got) != 3 {
		t.Fatalf("governed set changed to %v; update the security claim with it", got)
	}
}
