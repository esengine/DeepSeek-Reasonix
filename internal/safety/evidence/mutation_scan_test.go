package evidence

import (
	"encoding/json"
	"testing"
)

func bashArgs(command string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestUnprovenSegmentAcceptsAProvableChain(t *testing.T) {
	if _, unproven := bashUnprovenSegment("ls -la && go test ./..."); unproven {
		t.Fatal("a read-only listing beside a verification was reported as unprovable")
	}
}
