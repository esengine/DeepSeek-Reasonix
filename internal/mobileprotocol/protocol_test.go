package mobileprotocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvelopeRoundTripOmitsEmpty(t *testing.T) {
	e := NewEnvelope(TypePing)
	b, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, banned := range []string{`"requestId"`, `"sessionId"`, `"seq"`, `"ack"`, `"payload"`} {
		if strings.Contains(s, banned) {
			t.Fatalf("expected omitempty for empty fields, got %s", s)
		}
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || got.Type != TypePing {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestEnvelopeRejectsFutureVersion(t *testing.T) {
	_, err := Decode([]byte(`{"version":99,"type":"ping"}`))
	if err == nil {
		t.Fatal("expected error for future version")
	}
}

func TestSessionDescriptorNormalizeLocal(t *testing.T) {
	d := SessionDescriptor{ID: "s1", Runtime: RuntimeLocal}
	d.Normalize()
	if d.Status != "idle" {
		t.Fatalf("status=%q", d.Status)
	}
	if len(d.Capabilities) != len(LocalCapabilities) {
		t.Fatalf("capabilities=%v", d.Capabilities)
	}
	// Local must not advertise shell/git.
	for _, c := range d.Capabilities {
		if c == "shell" || c == "git" || c == "stdio_mcp" {
			t.Fatalf("local capabilities must not include %q", c)
		}
	}
}

func TestSessionDescriptorJSONOmitempty(t *testing.T) {
	d := SessionDescriptor{ID: "s1", Runtime: RuntimeLocal, Revision: 1}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `"nodeId"`) || strings.Contains(s, `"title"`) {
		t.Fatalf("unexpected fields: %s", s)
	}
}

func TestValidRuntime(t *testing.T) {
	if !ValidRuntime(RuntimeLocal) || !ValidRuntime(RuntimeRemote) {
		t.Fatal("expected local/remote valid")
	}
	if ValidRuntime("hybrid") {
		t.Fatal("hybrid must be invalid — runtime is immutable dual mode only")
	}
}

func TestCommandPayloadCreateSession(t *testing.T) {
	args := CreateSessionArgs{Runtime: RuntimeRemote, ProviderRef: "openai/gpt-4.1", Title: "demo"}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := CommandPayload{Name: CmdCreateSession, Args: raw}
	env := NewEnvelope(TypeCommand)
	env.RequestID = "req-1"
	if err := env.MarshalPayload(cmd); err != nil {
		t.Fatal(err)
	}
	b, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	var out CommandPayload
	if err := got.UnmarshalPayload(&out); err != nil {
		t.Fatal(err)
	}
	if out.Name != CmdCreateSession {
		t.Fatalf("name=%q", out.Name)
	}
	var gotArgs CreateSessionArgs
	if err := json.Unmarshal(out.Args, &gotArgs); err != nil {
		t.Fatal(err)
	}
	if gotArgs.Runtime != RuntimeRemote || gotArgs.Title != "demo" {
		t.Fatalf("args=%+v", gotArgs)
	}
}

func TestLocalCapabilitiesOrderStable(t *testing.T) {
	// Cache-sensitive surfaces freeze tool order; treat this slice as a golden list.
	want := []string{"web_read", "attachment_read", "image_input", "http_mcp"}
	if len(LocalCapabilities) != len(want) {
		t.Fatalf("len=%d want %d", len(LocalCapabilities), len(want))
	}
	for i := range want {
		if LocalCapabilities[i] != want[i] {
			t.Fatalf("order drift at %d: got %q want %q", i, LocalCapabilities[i], want[i])
		}
	}
}
