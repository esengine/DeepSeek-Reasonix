package sidecar

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

func withTools(served string, capability bool) func(rt *pluginpkg.RuntimeSpec) {
	return func(rt *pluginpkg.RuntimeSpec) {
		if capability {
			rt.Capabilities = []string{"tools"}
		}
		rt.Tools = []pluginpkg.RuntimeTool{
			{Name: "lookup", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)},
			{Name: "fail", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)},
		}
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"2","name":"fake","version":"1","tools":` + served + `,"stateSchemaVersion":0}`
	}
}

// A declared tool runs over the wire with the model's arguments, and a failure
// the tool reports about its own work stays apart from a call that never ran.
func TestADeclaredToolRunsWithItsArguments(t *testing.T) {
	client := startFakeClient(t, withTools(`["lookup","fail"]`, true), nil)
	if !client.ServesTool("lookup") || client.ServesTool("other") {
		t.Fatalf("served = %v", client.Handshake().Tools)
	}
	if got := client.manifestExpectation().Tools; len(got) != 2 {
		t.Fatalf("the manifest expectation named %v, want both declared tools", got)
	}
	res, err := client.CallTool(context.Background(), "lookup", json.RawMessage(`{"q":"x"}`), 5*time.Second)
	if err != nil || res.IsError || res.Content != `ran lookup {"q":"x"}` {
		t.Fatalf("CallTool = %+v, %v", res, err)
	}
	res, err = client.CallTool(context.Background(), "fail", nil, 5*time.Second)
	if err != nil || !res.IsError || res.Content != "ran fail {}" {
		t.Fatalf("a failing tool = %+v, %v; want its own error, arguments defaulted to {}", res, err)
	}
}

// The model is shown the manifest's schemas, so a runtime cannot serve a tool
// its manifest does not describe, nor any tool without the tools capability.
func TestAToolOutsideTheManifestFailsTheHandshake(t *testing.T) {
	for name, configure := range map[string]func(*pluginpkg.RuntimeSpec){
		"undeclared tool":    withTools(`["lookup","rm_rf"]`, true),
		"missing capability": withTools(`["lookup"]`, false),
	} {
		t.Run(name, func(t *testing.T) {
			pkg, installed := fakeSidecarPackage(t, "fakeplugin", configure)
			client, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
			if err == nil {
				_ = client.Close()
				t.Fatal("the handshake was accepted")
			}
			if reason := protocolReason(t, err); reason != protocol.ErrCapabilityNotDeclared {
				t.Fatalf("reason = %q, want %q", reason, protocol.ErrCapabilityNotDeclared)
			}
		})
	}
}
