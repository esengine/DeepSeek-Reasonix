//go:build live

package agent

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/sessioncontext"
	"reasonix/internal/tool"
)

type liveSessionContextProvider struct {
	name, keyEnv, kind, baseURL, model string
	extra                              map[string]any
}

type liveSessionContextResult struct {
	prompt, hit, miss int
	systemHash        string
}

func TestLiveSessionContextFirstTurnMatrix(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_SESSION_CONTEXT_MATRIX") == "" {
		t.Skip("set REASONIX_LIVE_SESSION_CONTEXT_MATRIX=1 to run the paid live matrix")
	}
	providers := []liveSessionContextProvider{
		{name: "deepseek", keyEnv: "DEEPSEEK_API_KEY", kind: "anthropic", baseURL: "https://api.deepseek.com/anthropic", model: "deepseek-v4-flash", extra: map[string]any{"thinking": "disabled", "effort": "high"}},
		{name: "longcat", keyEnv: "LONGCAT_API_KEY", kind: "openai", baseURL: "https://api.longcat.chat/openai/v1", model: "LongCat-2.0", extra: map[string]any{"thinking": "disabled"}},
		{name: "zhipu-coding", keyEnv: "ZHIPU_CODING_API_KEY", kind: "openai", baseURL: "https://api.z.ai/api/coding/paas/v4", model: "glm-5.1", extra: map[string]any{"thinking": "disabled"}},
		{name: "opencode-go", keyEnv: "OPENCODE_GO_API_KEY", kind: "openai", baseURL: "https://opencode.ai/zen/go/v1", model: "glm-5.3", extra: map[string]any{"reasoning_protocol": "openai", "effort": "low"}},
	}
	stages := []struct {
		name     string
		sections sessioncontext.Sections
	}{
		{name: "same-project", sections: liveContextSections("workspace-a", "memory-v1", "alpha — initial skill")},
		{name: "workspace-changed", sections: liveContextSections("workspace-b", "memory-v1", "alpha — initial skill")},
		{name: "memory-changed", sections: liveContextSections("workspace-b", "memory-v2", "alpha — initial skill")},
		{name: "skill-added", sections: liveContextSections("workspace-b", "memory-v2", "alpha — initial skill\nbeta — added skill")},
		{name: "skill-edited", sections: liveContextSections("workspace-b", "memory-v2", "alpha — initial skill\nbeta — edited skill")},
		{name: "skill-deleted", sections: liveContextSections("workspace-b", "memory-v2", "beta — edited skill")},
	}

	for _, providerCase := range providers {
		providerCase := providerCase
		t.Run(providerCase.name, func(t *testing.T) {
			key := strings.TrimSpace(os.Getenv(providerCase.keyEnv))
			if key == "" {
				t.Skip(providerCase.keyEnv + " not set")
			}
			prov := newLiveSessionContextProvider(t, providerCase, key)
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}
			var stableSystemHash string
			call := 0
			for _, stage := range stages {
				snapshot := sessioncontext.Build(stage.sections)
				for repeat := 1; repeat <= 3; repeat++ {
					call++
					result := runLiveSessionContextFirstTurn(t, prov, snapshot, call)
					if stableSystemHash == "" {
						stableSystemHash = result.systemHash
					} else if result.systemHash != stableSystemHash {
						t.Fatalf("stage=%s repeat=%d changed system hash", stage.name, repeat)
					}
					t.Logf("provider=%s stage=%s repeat=%d prompt=%d hit=%d miss=%d digest=%s",
						providerCase.name, stage.name, repeat, result.prompt, result.hit, result.miss, snapshot.Digest[:12])
					time.Sleep(750 * time.Millisecond)
				}
			}
		})
	}
}

func liveContextSections(workspace, memory, skills string) sessioncontext.Sections {
	return sessioncontext.Sections{
		Environment:      "runtime: live-provider-matrix\noffline: false",
		Workspace:        "Current workspace: " + workspace,
		BackgroundMemory: memory,
		SkillsCatalog:    skills,
	}
}

func newLiveSessionContextProvider(t *testing.T, cfg liveSessionContextProvider, key string) provider.Provider {
	t.Helper()
	extra := make(map[string]any, len(cfg.extra)+1)
	for name, value := range cfg.extra {
		extra[name] = value
	}
	extra["api_key_env"] = cfg.keyEnv
	providerConfig := provider.Config{Name: cfg.name, BaseURL: cfg.baseURL, Model: cfg.model, APIKey: key, Extra: extra}
	var (
		prov provider.Provider
		err  error
	)
	if cfg.kind == "anthropic" {
		prov, err = anthropic.New(providerConfig)
	} else {
		prov, err = openai.New(providerConfig)
	}
	if err != nil {
		t.Fatalf("new %s provider: %v", cfg.name, err)
	}
	return prov
}

func runLiveSessionContextFirstTurn(t *testing.T, prov provider.Provider, snapshot sessioncontext.Snapshot, call int) liveSessionContextResult {
	t.Helper()
	system := "You are a concise cache verification assistant. " +
		strings.Repeat("Keep this stable policy prefix byte-identical and answer the final request briefly. ", 90)
	var usage *provider.Usage
	var diagnostics *event.CacheDiagnostics
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Usage {
			return
		}
		if e.Usage != nil {
			copyUsage := *e.Usage
			usage = &copyUsage
		}
		if e.CacheDiagnostics != nil {
			copyDiagnostics := *e.CacheDiagnostics
			diagnostics = &copyDiagnostics
		}
	})
	session := NewSession(system)
	agent := New(prov, tool.NewRegistry(), session, Options{MaxSteps: 2, MaxOutputTokens: 64}, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ctx = WithTurnContextBundle(ctx, TurnContextBundle{Executor: snapshot})
	if err := agent.Run(ctx, fmt.Sprintf("Reply with exactly OK. Matrix call %d.", call)); err != nil {
		t.Fatalf("live first turn %d: %v", call, err)
	}
	messages := session.Snapshot()
	if len(messages) < 4 || messages[0].Role != provider.RoleSystem || messages[1].Origin != provider.MessageOriginHost || messages[2].Origin != provider.MessageOriginUser {
		t.Fatalf("live first turn %d history order = %+v", call, messages)
	}
	parsed, ok := sessioncontext.Parse(messages[1].Content)
	if !ok || parsed.Digest != snapshot.Digest {
		t.Fatalf("live first turn %d persisted invalid context", call)
	}
	if strings.TrimSpace(messages[len(messages)-1].Content) == "" {
		t.Fatalf("live first turn %d returned no assistant text", call)
	}
	if usage == nil || usage.PromptTokens == 0 {
		t.Fatalf("live first turn %d returned no usage", call)
	}
	if diagnostics == nil || diagnostics.SessionContext == nil || diagnostics.SessionContext.Digest != snapshot.Digest ||
		diagnostics.SessionContext.TargetRole != "executor" || !slices.Contains(diagnostics.SessionContext.Reasons, "first_seen") {
		t.Fatalf("live first turn %d diagnostics = %+v", call, diagnostics)
	}
	return liveSessionContextResult{
		prompt: usage.PromptTokens, hit: usage.CacheHitTokens, miss: usage.CacheMissTokens,
		systemHash: diagnostics.SystemHash,
	}
}
