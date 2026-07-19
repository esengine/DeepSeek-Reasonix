package mobilecore

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/mobileprotocol"
)

func TestCreateSessionLocalOnly(t *testing.T) {
	c := New()
	out := c.CreateSessionJSON(`{"runtime":"remote","title":"x"}`)
	if !strings.Contains(out, "invalid_runtime") {
		t.Fatalf("expected reject remote: %s", out)
	}
	out = c.CreateSessionJSON(`{"title":"hello","providerRef":"openai/gpt"}`)
	var d mobileprotocol.SessionDescriptor
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err, out)
	}
	if d.Runtime != mobileprotocol.RuntimeLocal || d.ID == "" {
		t.Fatalf("%+v", d)
	}
	if len(d.Capabilities) != len(mobileprotocol.LocalCapabilities) {
		t.Fatalf("capabilities not frozen: %v", d.Capabilities)
	}
	// Order golden — cache-sensitive when later mapped to tools.
	for i, want := range mobileprotocol.LocalCapabilities {
		if d.Capabilities[i] != want {
			t.Fatalf("capability order drift: %v", d.Capabilities)
		}
	}
}

func TestSubmitAndSnapshot(t *testing.T) {
	c := New()
	var d mobileprotocol.SessionDescriptor
	if err := json.Unmarshal([]byte(c.CreateSessionJSON(`{"title":"t"}`)), &d); err != nil {
		t.Fatal(err)
	}
	ack := c.SubmitJSON(d.ID, `{"text":"hi"}`, "req-1")
	if strings.Contains(ack, `"error"`) {
		t.Fatal(ack)
	}
	snap := c.SnapshotJSON(d.ID)
	var payload mobileprotocol.SnapshotPayload
	if err := json.Unmarshal([]byte(snap), &payload); err != nil {
		t.Fatal(err, snap)
	}
	if payload.LastEventSeq != 1 {
		t.Fatalf("seq=%d", payload.LastEventSeq)
	}
}

func TestProbeProviderHTTPSPolicy(t *testing.T) {
	c := New()
	if out := c.ProbeProviderJSON(`{"baseUrl":"http://evil.example"}`); !strings.Contains(out, "insecure") && !strings.Contains(out, "http") {
		t.Fatalf("expected reject: %s", out)
	}
	if out := c.ProbeProviderJSON(`{"baseUrl":"http://127.0.0.1:8080","allowInsecureHttp":true}`); strings.Contains(out, `"error"`) {
		t.Fatalf("localhost http should be allowed: %s", out)
	}
	if out := c.ProbeProviderJSON(`{"baseUrl":"https://api.openai.com","providerRef":"openai"}`); strings.Contains(out, `"error"`) {
		t.Fatalf("https should be ok: %s", out)
	}
}

func TestLocalCapabilitiesJSONStable(t *testing.T) {
	var caps []string
	if err := json.Unmarshal([]byte(LocalCapabilitiesJSON()), &caps); err != nil {
		t.Fatal(err)
	}
	want := []string{"web_read", "attachment_read", "image_input", "http_mcp"}
	if strings.Join(caps, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", caps)
	}
}
