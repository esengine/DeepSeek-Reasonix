package provider

import (
	"encoding/json"
	"testing"
)

// 1.x logs mark a message "origin":"host" or "user". Reading one must not fail
// on the host's own turn origin, which is stored under another name.
func TestA1xOriginStringStillDecodes(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hi","origin":"host"}`), &m); err != nil {
		t.Fatalf("a 1.x message no longer decodes: %v", err)
	}
	if m.Origin != nil {
		t.Fatalf("1.x's host/user marker was read as a turn origin: %+v", m.Origin)
	}
	in := Message{Role: RoleUser, Content: "x", Origin: &Origin{Kind: "message", Agent: "a-1"}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Message
	if err := json.Unmarshal(data, &out); err != nil || out.Origin == nil || out.Origin.Agent != "a-1" {
		t.Fatalf("origin did not round-trip: %s → %+v, %v", data, out.Origin, err)
	}
}
