package boot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func buildFullsidecarPackage(t *testing.T) string {
	t.Helper()
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "sdk", "go", "examples", "fullsidecar"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	binary := filepath.Join(source, "bin", "full-sidecar")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	cmd.Dir = example
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build SDK fullsidecar: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile(filepath.Join(example, pluginpkg.NativeManifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, pluginpkg.NativeManifest), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return source
}

func fullsidecarSurface(t *testing.T, rec *uiSinkRecorder, id string) event.ExtensionSurfacePayload {
	t.Helper()
	var found event.ExtensionSurfacePayload
	waitForCond(t, "fullsidecar surface "+id, 10*time.Second, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, ev := range rec.events {
			if ev.Extension != nil && ev.Extension.PluginID == "full-sidecar" && ev.Extension.SurfaceID == id {
				found = *ev.Extension
				return true
			}
		}
		return false
	})
	return found
}

func TestEffectFullsidecarInstalledHostSurfaces(t *testing.T) {
	source := buildFullsidecarPackage(t)
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "FULLSIDECAR BASE"

[environment]
enabled = false

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-effect-fullsidecar"
model = "x"
`)
	approveWorkspace(t, workspace)
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	install := func(args map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			OK      bool   `json:"ok"`
			Applied bool   `json:"applied"`
			Status  string `json:"status"`
			PlanID  string `json:"planId"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK {
			t.Fatalf("install_source = %s, err=%v", out, err)
		}
		if args["apply"] == true || args["op"] == "uninstall" {
			if !result.Applied || result.Status != "done" {
				t.Fatalf("install_source did not apply: %s", out)
			}
		} else if result.Applied || result.Status != "planned" || result.PlanID == "" {
			t.Fatalf("install_source did not preview: %s", out)
		}
		return result.PlanID
	}
	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-fullsidecar", func(provider.Config) (provider.Provider, error) { return rec, nil })
	runPhase := func(t *testing.T, enabled bool) {
		t.Helper()
		sink := &uiSinkRecorder{}
		asks := make(chan event.Ask, 1)
		res, err := BuildRuntime(t.Context(), Options{Sink: event.FuncSink(func(ev event.Event) {
			sink.emit(ev)
			if ev.Kind == event.AskRequest {
				asks <- ev.Ask
			}
		})})
		if err != nil {
			t.Fatal(err)
		}
		defer res.Controller.Close()
		if enabled {
			if res.Extensions == nil || res.Extensions.Client("full-sidecar") == nil {
				t.Fatal("installed fullsidecar did not start")
			}
			client := res.Extensions.Client("full-sidecar")
			defer func() {
				res.Controller.Close()
				waitForCond(t, "fullsidecar process exit", 10*time.Second, client.Exited)
				if !res.Runtime.Closed() {
					t.Fatal("fullsidecar runtime did not close")
				}
			}()
		} else if res.Extensions != nil || res.ExtensionUI != nil {
			t.Fatal("absent, disabled, or removed fullsidecar still has runtime resources")
		}
		before := len(rec.requests())
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if err := res.Controller.Run(ctx, "reply ok"); err != nil {
			t.Fatal(err)
		}
		reqs := agentRequests(rec.requests()[before:])
		if len(reqs) == 0 {
			t.Fatal("turn never reached the recording provider")
		}
		prompt := systemText(reqs[0])
		if strings.Contains(prompt, "You are Reasonix running under the fullsidecar demo strategy.") != enabled || !strings.Contains(prompt, "FULLSIDECAR BASE") {
			t.Fatalf("enabled=%t, provider system prompt = %q", enabled, prompt)
		}
		var declared bool
		for _, descriptor := range res.ProviderResolver.Catalog() {
			if descriptor.Ref == "plugin/full-sidecar/fake/echo" {
				declared = true
				if descriptor.Model != "echo" || !descriptor.Tools {
					t.Fatalf("fullsidecar provider descriptor = %+v", descriptor)
				}
			}
		}
		if declared != enabled {
			t.Fatalf("enabled=%t, fullsidecar provider declared=%t", enabled, declared)
		}
		model, resolveErr := res.ProviderResolver.Resolve(provider.Selection{Ref: "plugin/full-sidecar/fake/echo"})
		if !enabled {
			if resolveErr == nil {
				t.Fatal("inactive fullsidecar provider still resolves")
			}
		} else {
			if resolveErr != nil {
				t.Fatalf("resolve installed SDK provider: %v", resolveErr)
			}
			for range 2 {
				stream, err := model.Stream(ctx, provider.Request{
					Messages:  []provider.Message{{Role: provider.RoleUser, Content: "say hi"}},
					MaxTokens: 32,
				})
				if err != nil {
					t.Fatalf("stream installed SDK provider: %v", err)
				}
				chunks := collectProviderChunks(t, stream)
				wantTypes := []provider.ChunkType{provider.ChunkText, provider.ChunkText, provider.ChunkToolCall, provider.ChunkUsage, provider.ChunkDone}
				if len(chunks) != len(wantTypes) {
					t.Fatalf("SDK provider chunks = %+v", chunks)
				}
				for i, kind := range wantTypes {
					if chunks[i].Type != kind || chunks[i].Err != nil {
						t.Fatalf("SDK provider chunk %d = %+v, want %v", i, chunks[i], kind)
					}
				}
				if chunks[0].Text != "fake-hello " || chunks[1].Text != "fake-world" {
					t.Fatalf("SDK provider text = %+v", chunks[:2])
				}
				call := chunks[2].ToolCall
				if call == nil || call.ID != "call-1" || call.Name != "lookup" || call.Arguments != `{"query":"reasonix"}` {
					t.Fatalf("SDK provider tool call = %+v", call)
				}
				usage := chunks[3].Usage
				if usage == nil || usage.PromptTokens != 5 || usage.CompletionTokens != 7 || usage.TotalTokens != 12 ||
					usage.CacheHitTokens != 2 || usage.CacheMissTokens != 3 || usage.ReasoningTokens != 4 || usage.FinishReason != "stop" {
					t.Fatalf("SDK provider usage = %+v", usage)
				}
			}
		}
		actions := res.Controller.ExtensionActions()
		if !enabled {
			if len(actions) != 0 {
				t.Fatalf("inactive fullsidecar actions = %+v", actions)
			}
			return
		}
		if len(actions) != 1 || actions[0].Slash != "/full-sidecar:demo" {
			t.Fatalf("fullsidecar actions = %+v", actions)
		}
		status := fullsidecarSurface(t, sink, "fullsidecar-status")
		if status.Kind != event.ExtensionSurfaceStatus || status.Status == nil || status.Status.Label != "fullsidecar online" {
			t.Fatalf("status at frontend sink = %+v", status)
		}
		card := fullsidecarSurface(t, sink, "fullsidecar-card")
		if card.Kind != event.ExtensionSurfaceCard || card.Card == nil || card.Card.Title != "fullsidecar" || len(card.Card.Actions) != 1 || card.Card.Actions[0].ActionID != "demo" {
			t.Fatalf("card at frontend sink = %+v", card)
		}
		done := make(chan error, 1)
		go func() {
			_, err := res.Controller.InvokeExtensionAction(ctx, actions[0].Slash, nil)
			done <- err
		}()
		select {
		case ask := <-asks:
			if len(ask.Questions) != 2 || ask.Questions[0].ID != "name" || ask.Questions[1].ID != "loud" || len(ask.Questions[1].Options) != 2 {
				t.Fatalf("demo form at frontend sink = %+v", ask)
			}
			res.Controller.AnswerQuestion(ask.ID, []event.AskAnswer{
				{QuestionID: "name", Selected: []string{"SDK author"}},
				{QuestionID: "loud", Selected: []string{ask.Questions[1].Options[0].Label}},
			})
		case err := <-done:
			t.Fatalf("demo action returned without asking: %v", err)
		case <-ctx.Done():
			t.Fatal("demo form never reached the frontend sink")
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("demo action: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("demo action did not finish after the frontend answered")
		}
		greeting := fullsidecarSurface(t, sink, "fullsidecar-greeting")
		if greeting.Kind != event.ExtensionSurfaceNotification || greeting.Notification == nil || greeting.Notification.Title != "HELLO, SDK AUTHOR!" {
			t.Fatalf("greeting at frontend sink = %+v", greeting)
		}
	}
	t.Run("absent", func(t *testing.T) { runPhase(t, false) })
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	args["planId"] = install(args)
	args["apply"] = true
	install(args)
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	t.Run("installed", func(t *testing.T) { runPhase(t, true) })
	if err := pluginpkg.SetEnabled(reasonixHome, "full-sidecar", false); err != nil {
		t.Fatal(err)
	}
	t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "full-sidecar", true); err != nil {
		t.Fatal(err)
	}
	t.Run("reenabled", func(t *testing.T) { runPhase(t, true) })
	install(map[string]any{"op": "uninstall", "name": "full-sidecar", "scope": "global"})
	t.Run("removed", func(t *testing.T) { runPhase(t, false) })
}
