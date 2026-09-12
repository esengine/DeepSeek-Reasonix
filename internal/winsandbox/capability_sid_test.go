package winsandbox

import (
	"regexp"
	"testing"
)

func TestCapabilitySIDIsDeterministicAndValid(t *testing.T) {
	first := deriveCapabilitySID(capabilityWorkspace, `c:\work\reasonix`)
	second := deriveCapabilitySID(capabilityWorkspace, `c:\work\reasonix`)
	if first != second {
		t.Fatalf("capability SID is not deterministic: %q != %q", first, second)
	}
	if !regexp.MustCompile(`^S-1-4-[1-9][0-9]*-[1-9][0-9]*$`).MatchString(first) {
		t.Fatalf("capability SID has unexpected form: %q", first)
	}
}

func TestCapabilitySIDSeparatesPurposeAndPath(t *testing.T) {
	workspace := deriveCapabilitySID(capabilityWorkspace, `c:\work\reasonix`)
	temp := deriveCapabilitySID(capabilitySessionTemp, `c:\work\reasonix`)
	otherWorkspace := deriveCapabilitySID(capabilityWorkspace, `c:\work\other`)

	if workspace == temp {
		t.Fatalf("workspace and session-temp capabilities must be domain separated: %q", workspace)
	}
	if workspace == otherWorkspace {
		t.Fatalf("different roots must receive different capabilities: %q", workspace)
	}
}

func TestCapabilitySIDSubauthoritiesStayInThirtyBitRange(t *testing.T) {
	first, second := capabilitySubauthorities(capabilityWorkspace, `c:\work\reasonix`)
	const max = uint32(1<<30 - 1)
	if first < 1 || first > max || second < 1 || second > max {
		t.Fatalf("subauthorities outside 1..2^30-1: %d, %d", first, second)
	}
}
