package boot

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension/protocol"
)

func TestEffectFailedSwitchRestoresSidecars(t *testing.T) {
	for _, addPackage := range []bool{false, true} {
		name := "unchanged-graph"
		if addPackage {
			name = "added-package"
		}
		t.Run(name, func(t *testing.T) {
			isolateConfigHome(t)
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			writePluginDefaultFixture(t, workspace, "plugin/stable/fake/x")
			home := config.ReasonixHomeDir()
			installProviderFake(t, home, "stable", nil)
			serving, err := BuildRuntime(t.Context(), Options{Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(serving.Controller.Close)
			client := serving.Extensions.Client("stable")
			if client == nil {
				t.Fatal("serving runtime has no provider sidecar")
			}
			generation := serving.Controller.RuntimeGeneration()
			pidFile := filepath.Join(workspace, "added.pid")
			if addPackage {
				installBootFakePlugin(t, home, "added", map[string]any{
					"env": map[string]string{bootFakeEnvPIDFile: pidFile},
				})
			}

			// Mirror serve.reuseFromLastBuild, including the published generation.
			opts := Options{Model: "plugin/missing/fake/x", Sink: event.Discard}
			opts.RuntimeReload = RuntimeReload{
				ForceFullRebuild:   true,
				Extensions:         serving.Extensions,
				Graph:              serving.Plan.Graph,
				Generation:         serving.Snapshot.Generation(),
				Owner:              serving.Owner,
				PreviousSnapshot:   serving.Snapshot,
				PreviousDispatcher: serving.Dispatcher,
				PreviousPlan:       serving.Plan,
				ReuseAssembly:      serving.Assembly,
			}
			_, err = BuildRuntime(t.Context(), opts)
			if !errors.Is(err, ErrUnknownModel) {
				t.Fatalf("failed switch = %v, want ErrUnknownModel", err)
			}
			if addPackage {
				pid := readFakePID(t, pidFile)
				waitForCond(t, "failed build's new sidecar exit", 10*time.Second, func() bool { return !pidAlive(pid) })
			}
			if serving.Extensions.Client("stable") != client || client.Exited() {
				t.Fatal("failed switch did not restore the serving sidecar")
			}
			if serving.Runtime.Closed() || serving.Controller.RuntimeGeneration() != generation {
				t.Fatal("failed switch changed the serving runtime")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			result, err := client.Intercept(ctx, protocol.EventSessionStart, json.RawMessage(`{}`), time.Second)
			if err != nil || result.Decision != protocol.DecisionContinue {
				t.Fatalf("serving sidecar after failed switch = %+v, %v", result, err)
			}
			if err := serving.Controller.RunTurn(ctx, "reply after failed switch"); err != nil {
				t.Fatalf("provider turn after failed switch: %v", err)
			}
			history := serving.Controller.History()
			if last := history[len(history)-1]; last.Role != provider.RoleAssistant || last.Content != "fake-hello fake-world" {
				t.Fatalf("provider reply after failed switch = %+v", last)
			}
		})
	}
}
