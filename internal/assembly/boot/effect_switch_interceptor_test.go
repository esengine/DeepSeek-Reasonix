package boot

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestEffectModelSwitchKeepsAdoptedInterceptors(t *testing.T) {
	for _, required := range []bool{false, true} {
		name := "optional"
		if required {
			name = "required"
		}
		t.Run(name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			rec := &effectRecordingProvider{}
			kind := "switch-interceptor-effect-" + name
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, dir, "reasonix.toml", `
default_model = "model-a"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "model-a"
kind = "`+kind+`"
model = "x"

[[providers]]
name = "model-b"
kind = "`+kind+`"
model = "y"
`)
			approveWorkspace(t, dir)
			installBootFakePlugin(t, config.ReasonixHomeDir(), "rewriter", map[string]any{
				"required":   required,
				"intercepts": []string{"input.receive"},
				"env":        map[string]string{bootFakeEnvReplaceInput: "EXTENSION INPUT"},
			})
			sink := &noticeSink{}
			serving, err := BuildRuntime(t.Context(), Options{Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(serving.Controller.Close)
			client := serving.Extensions.Client("rewriter")
			if client == nil {
				t.Fatal("initial build has no interceptor sidecar")
			}
			t.Cleanup(func() {
				serving.Controller.Close()
				waitForCond(t, "adopted interceptor process exit", 10*time.Second, client.Exited)
			})
			hash := serving.Snapshot.CacheHash()
			schemas, err := json.Marshal(serving.Snapshot.ToolSchemas())
			if err != nil {
				t.Fatal(err)
			}
			assertInput := func() {
				t.Helper()
				if err := serving.Controller.RunTurn(t.Context(), "original input"); err != nil {
					t.Fatalf("turn with adopted interceptor: %v", err)
				}
				reqs := rec.requests()
				if len(reqs) == 0 {
					t.Fatal("turn did not reach the provider")
				}
				messages := reqs[len(reqs)-1].Messages
				for _, message := range slices.Backward(messages) {
					if message.Role == provider.RoleUser {
						if !strings.Contains(message.Content, "EXTENSION INPUT") || strings.Contains(message.Content, "original input") {
							t.Errorf("model %s received the original input instead of the extension's replacement", serving.Controller.ModelRef())
						}
						return
					}
				}
				t.Fatal("provider request has no user message")
			}
			assertInput()
			for _, model := range []string{"model-b/y", "model-a/x"} {
				// These are the live inputs serve.rebuildOptions/reuseFromLastBuild carry.
				opts := Options{Model: model, Sink: sink, WorkspaceRoot: dir, SessionDir: serving.Controller.SessionDir(), SessionTemp: serving.Controller.SessionTemp(), BrowserSession: serving.Controller.BrowserSession(), WorkspaceRepo: serving.Controller.WorkspaceRepo()}
				opts.RuntimeReload = RuntimeReload{
					ForceFullRebuild: true, Extensions: serving.Extensions, Owner: serving.Owner,
					Graph: serving.Plan.Graph, Generation: serving.Snapshot.Generation(),
					PreviousSnapshot: serving.Snapshot, PreviousDispatcher: serving.Dispatcher,
					PreviousPlan: serving.Plan, ReuseAssembly: serving.Assembly,
				}
				next, err := BuildRuntime(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(next.Controller.Close)
				if next.Extensions.Client("rewriter") != client || client.Exited() {
					t.Fatal("model switch did not keep the same live sidecar")
				}
				serving.Controller.Close()
				serving = next
				assertInput()
				nextSchemas, err := json.Marshal(next.Snapshot.ToolSchemas())
				if err != nil {
					t.Fatal(err)
				}
				if next.Snapshot.CacheHash() != hash || string(nextSchemas) != string(schemas) {
					t.Error("interceptor adoption changed the provider-visible prefix")
				}
			}
			if ev, found := sink.notice(event.NoticeCodeExtensionSkipped); found {
				t.Errorf("live adopted sidecar was reported missing: %s", ev.Text)
			}
			serving.Controller.Close()
			waitForCond(t, "adopted interceptor process exit", 10*time.Second, client.Exited)
		})
	}
}
