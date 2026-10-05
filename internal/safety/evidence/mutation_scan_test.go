package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

func bashArgs(command string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}
	return raw
}

// A block that does not say which segment tripped it makes the model rewrite
// the part it guesses is at fault, which is how one block becomes three.
func TestMixedBlockNamesTheSegment(t *testing.T) {
	got := ShellContractMixedMessage(bashArgs("sed -n '1p' out.txt; go vet ./... && go test ./..."))
	if !strings.Contains(got, "sed -n '1p' out.txt") {
		t.Fatalf("message = %q, want the unproven segment named", got)
	}
	// "cannot prove" rather than "changes state": an unrecognized command is
	// outside what the host can classify, and saying otherwise sends the model
	// looking for a write that may not exist.
	if !strings.Contains(got, "cannot prove") {
		t.Fatalf("message = %q, want the host's uncertainty stated honestly", got)
	}
	if strings.Contains(got, "state-changing segment") {
		t.Fatalf("message = %q, want no claim that the segment changes state", got)
	}
}

// Delivery gets the same name and a different way out. One observed delivery
// run was refused three times running by the unnamed version: it rewrote the
// command, moved to '&&', then to ';', and every refusal read identically.
func TestDeliveryMixedBlockNamesTheSegmentAndRefusesTheAndAndRoute(t *testing.T) {
	got := ShellContractMixedDeliveryMessage(bashArgs("sed -n '1p' out.txt && go vet ./... && go test ./..."))
	if !strings.Contains(got, "sed -n '1p' out.txt") {
		t.Fatalf("message = %q, want the unproven segment named", got)
	}
	// Ordinary mode's exit is '&&'. Delivery refuses the mixture whatever the
	// exit status does, so offering it there buys the same block again.
	if !strings.Contains(got, "refused too") {
		t.Fatalf("message = %q, want it to say the '&&' route is refused as well", got)
	}
	if strings.Contains(got, "Chain them with '&&'") {
		t.Fatalf("message = %q, must not offer a route delivery refuses", got)
	}
}

// Nothing to name is still a usable block: the generic text stands in.
func TestDeliveryMixedBlockFallsBackWithoutASegment(t *testing.T) {
	got := ShellContractMixedDeliveryMessage(json.RawMessage(`{"command":""}`))
	if !strings.HasPrefix(got, "blocked:") {
		t.Fatalf("message = %q, want a usable block", got)
	}
	if strings.Contains(got, "Chain them with '&&'") {
		t.Fatalf("fallback = %q, must not offer a route delivery refuses", got)
	}
}

// Nothing to name is still a usable block: the generic text stands in.
func TestMixedBlockFallsBackWithoutASegment(t *testing.T) {
	got := ShellContractMixedMessage(json.RawMessage(`{"command":""}`))
	if !strings.HasPrefix(got, "blocked:") {
		t.Fatalf("message = %q, want the generic mixed block", got)
	}
}

// A long segment is clipped: a whole command echoed back is noise, and the head
// of it is what the model needs to recognize.
func TestMixedBlockClipsALongSegment(t *testing.T) {
	long := "go generate " + strings.Repeat("./pkg/very-long-path-segment ", 20)
	got := ShellContractMixedMessage(bashArgs(long + "; go test ./..."))
	if len(got) > 800 {
		t.Fatalf("message length = %d, want the segment clipped", len(got))
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("message = %q, want the clip marked", got)
	}
}

func TestUnprovenSegmentAcceptsAProvableChain(t *testing.T) {
	if _, unproven := bashUnprovenSegment("ls -la && go test ./..."); unproven {
		t.Fatal("a read-only listing beside a verification was reported as unprovable")
	}
}
