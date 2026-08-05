package sidecar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/pluginpkg"
)

// bigInputPayload builds an input.receive payload whose text is size bytes.
func bigInputPayload(size int) json.RawMessage {
	payload, _ := json.Marshal(map[string]string{"text": strings.Repeat("x", size)})
	return payload
}

// TestInterceptExternalizesLargePayload proves the outbound content-ref rule:
// a >64 KiB payload leaves the host as a null placeholder plus envelope, the
// extension pages the real bytes back through host/content/read, and its
// >64 KiB inline replacement comes home verified.
func TestInterceptExternalizesLargePayload(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "content_roundtrip"
	}, nil)
	payload := bigInputPayload(protocol.ExternalizeFieldBytes + 32<<10)
	result, err := client.Intercept(context.Background(), protocol.EventInputReceive, payload, 15*time.Second)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Decision != protocol.DecisionReplace {
		t.Fatalf("decision = %q, want replace", result.Decision)
	}
	var replacement struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result.Replacement, &replacement); err != nil {
		t.Fatalf("replacement decode: %v", err)
	}
	sum := sha256.Sum256(payload)
	want := fmt.Sprintf("read %d bytes sha256:%s", len(payload), hex.EncodeToString(sum[:]))
	if !strings.Contains(replacement.Text, want) {
		t.Fatalf("replacement text does not prove the extension read the content ref: want substring %q", want)
	}
	if len(result.Replacement) <= protocol.ExternalizeFieldBytes {
		t.Fatalf("replacement = %d bytes, want above the %d byte threshold", len(result.Replacement), protocol.ExternalizeFieldBytes)
	}
}

// TestInterceptSmallPayloadStaysInline proves the passthrough: below the
// threshold the payload travels inline and no envelope is produced.
func TestInterceptSmallPayloadStaysInline(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "content_roundtrip"
	}, nil)
	payload := json.RawMessage(`{"text":"small"}`)
	result, err := client.Intercept(context.Background(), protocol.EventInputReceive, payload, 5*time.Second)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	var replacement struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result.Replacement, &replacement); err != nil {
		t.Fatalf("replacement decode: %v", err)
	}
	// The fake reports the byte count it read; an inline payload arrives
	// directly, so the count matches the payload's exact length.
	want := fmt.Sprintf("read %d bytes", len(payload))
	if !strings.Contains(replacement.Text, want) {
		t.Fatalf("small payload did not arrive inline: replacement text lacks %q", want)
	}
}

// TestInterceptResolvesExternalizedReplacement proves the inbound content-ref
// rule: an extension may answer with replacement:null plus an envelope naming
// a host-held content ref, and the host pages it back out of the connection's
// store (host/content/read chunking) before the payload is handed over.
func TestInterceptResolvesExternalizedReplacement(t *testing.T) {
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "content_echo_ref"
	}, nil)
	payload := bigInputPayload(protocol.ExternalizeFieldBytes + 64<<10)
	result, err := client.Intercept(context.Background(), protocol.EventInputReceive, payload, 15*time.Second)
	if err != nil {
		t.Fatalf("Intercept: %v", err)
	}
	if result.Decision != protocol.DecisionReplace {
		t.Fatalf("decision = %q, want replace", result.Decision)
	}
	if string(result.Replacement) != string(payload) {
		t.Fatalf("resolved replacement = %d bytes, want the original %d byte payload", len(result.Replacement), len(payload))
	}
}

// TestInterceptRejectsPayloadBeyondObjectCap proves the hard ceiling: a
// payload beyond protocol.ContentRefObjectBytes cannot be externalized and
// fails with the frozen frame_too_large reason before touching the wire.
func TestInterceptRejectsPayloadBeyondObjectCap(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	payload := bigInputPayload(protocol.ContentRefObjectBytes + 1)
	_, err := client.Intercept(context.Background(), protocol.EventInputReceive, payload, 5*time.Second)
	if err == nil {
		t.Fatal("Intercept accepted a payload beyond ContentRefObjectBytes")
	}
	if reason := protocolReason(t, err); reason != protocol.ErrFrameTooLarge {
		t.Fatalf("reason = %q, want %q", reason, protocol.ErrFrameTooLarge)
	}
}

// TestNotifyEventExternalizesLargePayload covers the event-notification share
// of the content-ref rule at the envelope level (the wire path is the same
// helper Intercept exercises end to end).
func TestNotifyEventExternalizesLargePayload(t *testing.T) {
	client := &Client{store: NewStore()}
	params := protocol.EventParams{Event: protocol.EventSessionStart, Payload: bigInputPayload(protocol.ExternalizeFieldBytes + 1024)}
	if err := client.externalizeEventParams(&params); err != nil {
		t.Fatalf("externalizeEventParams: %v", err)
	}
	if len(params.Payload) != 0 {
		t.Fatal("large event payload did not move to a null placeholder")
	}
	if len(params.Externalized) != 1 || params.Externalized[0].JSONPointer != "/payload" {
		t.Fatalf("envelope = %+v", params.Externalized)
	}
	descriptor := params.Externalized[0]
	reassembled, err := client.store.readAll(descriptor.ContentRef)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if int64(len(reassembled)) != descriptor.TotalBytes {
		t.Fatalf("reassembled %d bytes, want %d", len(reassembled), descriptor.TotalBytes)
	}
	sum := sha256.Sum256(reassembled)
	if hex.EncodeToString(sum[:]) != descriptor.SHA256 {
		t.Fatal("descriptor SHA-256 does not match the stored object")
	}

	small := protocol.EventParams{Event: protocol.EventSessionStart, Payload: json.RawMessage(`{"at":1}`)}
	if err := client.externalizeEventParams(&small); err != nil {
		t.Fatalf("externalizeEventParams small: %v", err)
	}
	if string(small.Payload) != `{"at":1}` || len(small.Externalized) != 0 {
		t.Fatalf("small event payload = %s envelope %+v, want inline passthrough", small.Payload, small.Externalized)
	}
}

// TestResolveExternalizedReplacementValidation pins the envelope validation:
// inline-plus-envelope, unknown refs, byte-count and digest mismatches, and
// over-cap descriptors are all protocol errors, never silent decodes.
func TestResolveExternalizedReplacementValidation(t *testing.T) {
	client := &Client{store: NewStore()}
	content := []byte(`{"text":"stored"}`)
	ref, digest, totalBytes, err := client.store.Put(content)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	descriptor := func() ExternalizedField {
		return ExternalizedField{JSONPointer: "/replacement", ContentRef: ref, TotalBytes: totalBytes, SHA256: digest}
	}

	// Happy path: the replacement is reassembled and verified.
	result := protocol.InterceptResult{Decision: protocol.DecisionReplace, Externalized: []ExternalizedField{descriptor()}}
	if err := client.resolveExternalizedReplacement(&result); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(result.Replacement) != string(content) {
		t.Fatalf("replacement = %s, want %s", result.Replacement, content)
	}

	// Inline replacement alongside an envelope is malformed.
	both := protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"text":"inline"}`), Externalized: []ExternalizedField{descriptor()}}
	if err := client.resolveExternalizedReplacement(&both); err == nil {
		t.Fatal("inline replacement plus envelope accepted")
	}

	// Unknown ref answers content_ref_expired.
	unknown := descriptor()
	unknown.ContentRef = "content_gone"
	result = protocol.InterceptResult{Decision: protocol.DecisionReplace, Externalized: []ExternalizedField{unknown}}
	if err := client.resolveExternalizedReplacement(&result); err == nil {
		t.Fatal("unknown content ref accepted")
	} else if reason := protocolReason(t, err); reason != protocol.ErrContentRefExpired {
		t.Fatalf("unknown ref reason = %q, want %q", reason, protocol.ErrContentRefExpired)
	}

	// Byte-count mismatch is a protocol error.
	short := descriptor()
	short.TotalBytes = totalBytes - 1
	result = protocol.InterceptResult{Decision: protocol.DecisionReplace, Externalized: []ExternalizedField{short}}
	if err := client.resolveExternalizedReplacement(&result); err == nil {
		t.Fatal("byte-count mismatch accepted")
	}

	// Digest mismatch is a protocol error.
	tampered := descriptor()
	tampered.SHA256 = strings.Repeat("0", 64)
	result = protocol.InterceptResult{Decision: protocol.DecisionReplace, Externalized: []ExternalizedField{tampered}}
	if err := client.resolveExternalizedReplacement(&result); err == nil {
		t.Fatal("SHA-256 mismatch accepted")
	}

	// A descriptor beyond the object cap is frame_too_large.
	oversize := descriptor()
	oversize.TotalBytes = protocol.ContentRefObjectBytes + 1
	result = protocol.InterceptResult{Decision: protocol.DecisionReplace, Externalized: []ExternalizedField{oversize}}
	if err := client.resolveExternalizedReplacement(&result); err == nil {
		t.Fatal("over-cap descriptor accepted")
	} else if reason := protocolReason(t, err); reason != protocol.ErrFrameTooLarge {
		t.Fatalf("over-cap reason = %q, want %q", reason, protocol.ErrFrameTooLarge)
	}

	// A pointer outside the schema's externalizable set is a protocol error.
	wrongPointer := descriptor()
	wrongPointer.JSONPointer = "/reason"
	result = protocol.InterceptResult{Decision: protocol.DecisionReplace, Externalized: []ExternalizedField{wrongPointer}}
	if err := client.resolveExternalizedReplacement(&result); err == nil {
		t.Fatal("externalized pointer outside the schema set accepted")
	}
}
