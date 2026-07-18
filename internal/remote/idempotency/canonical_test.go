package idempotency

import (
	"encoding/json"
	"testing"

	"reasonix/internal/remote/protocol"
)

type fingerprintParams struct {
	RequestID            protocol.RequestID     `json:"requestId"`
	ExpectedHostEpoch    protocol.HostEpoch     `json:"expectedHostEpoch"`
	Target               protocol.RuntimeTarget `json:"target"`
	ExpectedRuntimeEpoch protocol.RuntimeEpoch  `json:"expectedRuntimeEpoch"`
	Payload              json.RawMessage        `json:"payload"`
}

func TestCanonicalJSONSortsEveryObjectWithoutChangingArrays(t *testing.T) {
	left := json.RawMessage(`{"z":1,"nested":{"b":2,"a":1},"items":[{"y":2,"x":1},3]}`)
	right := json.RawMessage(`{"items":[{"x":1,"y":2},3],"nested":{"a":1,"b":2},"z":1}`)
	leftCanonical, err := CanonicalJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	rightCanonical, err := CanonicalJSON(right)
	if err != nil {
		t.Fatal(err)
	}
	if string(leftCanonical) != string(rightCanonical) {
		t.Fatalf("canonical order differs:\n%s\n%s", leftCanonical, rightCanonical)
	}
	const want = `{"items":[{"x":1,"y":2},3],"nested":{"a":1,"b":2},"z":1}`
	if string(leftCanonical) != want {
		t.Fatalf("canonical = %s, want %s", leftCanonical, want)
	}
}

func TestFingerprintOmitsOnlyTopLevelRequestID(t *testing.T) {
	target := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	left := fingerprintParams{
		RequestID: "request-a", ExpectedHostEpoch: "host-a",
		Target:               protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"},
		ExpectedRuntimeEpoch: "runtime-a",
		Payload:              json.RawMessage(`{"requestId":"nested","z":2,"a":1}`),
	}
	right := left
	right.RequestID = "request-b"
	right.Payload = json.RawMessage(`{"a":1,"z":2,"requestId":"nested"}`)

	leftFingerprint, err := FingerprintFor("session/submit", target, left.RequestID, left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := FingerprintFor("session/submit", target, right.RequestID, right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("requestId or JSON object order affected fingerprint:\n%s\n%s", leftFingerprint, rightFingerprint)
	}

	right.Payload = json.RawMessage(`{"a":1,"z":2,"requestId":"different"}`)
	changed, err := FingerprintFor("session/submit", target, right.RequestID, right)
	if err != nil {
		t.Fatal(err)
	}
	if changed == leftFingerprint {
		t.Fatal("nested requestId was incorrectly omitted")
	}
}

func TestFingerprintIncludesMethodTargetAndExpectedEpochs(t *testing.T) {
	target := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	params := fingerprintParams{
		RequestID: "request-a", ExpectedHostEpoch: "host-a",
		Target:               protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"},
		ExpectedRuntimeEpoch: "runtime-a", Payload: json.RawMessage(`{"value":1}`),
	}
	base, err := FingerprintFor("session/submit", target, params.RequestID, params)
	if err != nil {
		t.Fatal(err)
	}
	method, _ := FingerprintFor("session/steer", target, params.RequestID, params)
	otherTarget, _ := FingerprintFor("session/submit", SessionTarget(protocol.RuntimeTarget{
		WorkspaceID: "workspace-a", SessionID: "session-b",
	}), params.RequestID, params)
	params.ExpectedHostEpoch = "host-b"
	hostEpoch, _ := FingerprintFor("session/submit", target, params.RequestID, params)
	params.ExpectedHostEpoch = "host-a"
	params.ExpectedRuntimeEpoch = "runtime-b"
	runtimeEpoch, _ := FingerprintFor("session/submit", target, params.RequestID, params)
	for name, fingerprint := range map[string]Fingerprint{
		"method": method, "target": otherTarget, "host epoch": hostEpoch, "runtime epoch": runtimeEpoch,
	} {
		if fingerprint == base {
			t.Fatalf("%s was omitted from fingerprint", name)
		}
	}
	if len(base.String()) != len("sha256:")+64 {
		t.Fatalf("fingerprint string = %q", base.String())
	}
}

func TestFingerprintRejectsMalformedRegistryIdentity(t *testing.T) {
	target := HostTarget()
	valid := map[string]any{"requestId": "request-a", "expectedHostEpoch": "host-a"}
	tests := []struct {
		name      string
		method    string
		requestID protocol.RequestID
		target    Target
		params    any
	}{
		{name: "missing request id", method: "workspace/open", requestID: "request-a", target: target, params: map[string]any{"expectedHostEpoch": "host-a"}},
		{name: "mismatched request id", method: "workspace/open", requestID: "request-b", target: target, params: valid},
		{name: "empty method", method: "", requestID: "request-a", target: target, params: valid},
		{name: "noncanonical method", method: " workspace/open", requestID: "request-a", target: target, params: valid},
		{name: "invalid target", method: "workspace/open", requestID: "request-a", target: Target{Kind: TargetSession, SessionID: "session-a"}, params: valid},
		{name: "array params", method: "workspace/open", requestID: "request-a", target: target, params: []any{"request-a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := FingerprintFor(test.method, test.target, test.requestID, test.params); err == nil {
				t.Fatal("accepted malformed mutation identity")
			}
		})
	}
}

func TestTargetConstructorsAndValidation(t *testing.T) {
	valid := []Target{
		HostTarget(), WorkspaceTarget("workspace-a"),
		SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"}),
	}
	for _, target := range valid {
		if err := target.Validate(); err != nil {
			t.Fatalf("%+v: %v", target, err)
		}
	}
	invalid := []Target{
		{}, {Kind: TargetHost, WorkspaceID: "workspace-a"},
		{Kind: TargetWorkspace}, {Kind: TargetWorkspace, WorkspaceID: "workspace-a", SessionID: "session-a"},
		{Kind: TargetSession, WorkspaceID: "workspace-a"},
	}
	for _, target := range invalid {
		if err := target.Validate(); err == nil {
			t.Fatalf("accepted invalid target %+v", target)
		}
	}
}
