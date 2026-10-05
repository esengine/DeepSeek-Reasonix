package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// A model or effort switch rebuilds the runtime while reusing the serving
// generation's sidecars and discovered assembly. Reuse must not freeze the
// provider-visible prefix: what the switched runtime sends has to match a cold
// build of the same model, byte for byte, or the switch quietly keeps serving
// the previous generation's contract.
func TestEffectSwitchReusingGenerationMatchesColdBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	recA, recB := &effectRecordingProvider{}, &effectRecordingProvider{}
	provider.Register("switch-effect-a", func(provider.Config) (provider.Provider, error) { return recA, nil })
	provider.Register("switch-effect-b", func(provider.Config) (provider.Provider, error) { return recB, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "model-a"

[agent]
system_prompt = "BASE"

[[providers]]
name = "model-a"
kind = "switch-effect-a"
model = "x"

[[providers]]
name = "model-b"
kind = "switch-effect-b"
model = "y"
`)
	approveWorkspace(t, dir)

	ctx := context.Background()
	serving, err := BuildRuntime(ctx, Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	defer serving.Controller.Close()

	cold, err := BuildRuntime(ctx, Options{Model: "model-b/y", Sink: event.Discard})
	if err != nil {
		t.Fatalf("cold BuildRuntime: %v", err)
	}
	defer cold.Controller.Close()
	if err := cold.Controller.Run(ctx, "reply ok"); err != nil {
		t.Fatalf("cold Run: %v", err)
	}

	// Exactly what serve hands back for a switch (see reuseFromLastBuild).
	opts := Options{Model: "model-b/y", Sink: event.Discard}
	opts.RuntimeReload = RuntimeReload{
		ForceFullRebuild:   true,
		Extensions:         serving.Extensions,
		Owner:              serving.Owner,
		PreviousSnapshot:   serving.Snapshot,
		PreviousDispatcher: serving.Dispatcher,
		PreviousPlan:       serving.Plan,
		ReuseAssembly:      serving.Assembly,
	}
	if serving.Plan != nil {
		opts.Graph = serving.Plan.Graph
	}
	switched, err := BuildRuntime(ctx, opts)
	if err != nil {
		t.Fatalf("switch BuildRuntime: %v", err)
	}
	defer switched.Controller.Close()
	if err := switched.Controller.Run(ctx, "reply ok"); err != nil {
		t.Fatalf("switched Run: %v", err)
	}

	if len(recA.requests()) != 0 {
		t.Fatalf("the outgoing model served %d requests after the switch", len(recA.requests()))
	}
	reqs := recB.requests()
	if len(reqs) != 2 {
		t.Fatalf("target model saw %d requests, want the cold build's and the switched one", len(reqs))
	}
	coldReq, switchedReq := reqs[0], reqs[1]
	if len(coldReq.Messages) == 0 || len(switchedReq.Messages) == 0 {
		t.Fatal("no messages reached the provider boundary")
	}
	if got, want := switchedReq.Messages[0].Content, coldReq.Messages[0].Content; got != want {
		t.Errorf("reused assembly changed the system prefix:\n switched: %.200q\n cold:     %.200q", got, want)
	}
	coldTools, switchedTools := toolNames(coldReq), toolNames(switchedReq)
	if len(coldTools) != len(switchedTools) {
		t.Fatalf("tool surface size %d after switch, want %d", len(switchedTools), len(coldTools))
	}
	for name := range coldTools {
		if !switchedTools[name] {
			t.Errorf("tool %q missing from the switched runtime's surface", name)
		}
	}
}
