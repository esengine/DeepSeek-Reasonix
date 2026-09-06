//go:build live

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
	"reasonix/internal/tool"
)

type multiProviderCase struct{ vendor, keyEnv, model, protocol, base, thinking, effort, reasoning string }

// Every credential is restricted to one documented vendor endpoint. Only echo
// is exposed to the model, and no request/response body or credential is logged.
func multiProviderCases() []multiProviderCase {
	var out []multiProviderCase
	add := func(vendor, env, base, protocol, thinking, effort, reasoning string, models ...string) {
		for _, model := range models {
			out = append(out, multiProviderCase{vendor, env, model, protocol, base, thinking, effort, reasoning})
		}
	}
	add("longcat", "LONGCAT_API_KEY", "https://api.longcat.chat/openai/v1", "chat", "enabled", "enabled", "", "LongCat-2.0")
	add("longcat", "LONGCAT_API_KEY", "https://api.longcat.chat/anthropic", "anthropic", "enabled", "enabled", "", "LongCat-2.0")
	glmModels := []string{"glm-5.3-flash", "glm-5.3", "glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.5-air"}
	add("glm", "GLM_PLAN_API_KEY", "https://open.bigmodel.cn/api/coding/paas/v4", "chat", "", "", "glm", glmModels...)
	add("glm", "GLM_PLAN_API_KEY", "https://open.bigmodel.cn/api/anthropic", "anthropic", "adaptive", "", "", glmModels...)
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go/v1", "chat", "", "low", "openai", "glm-5.3-flash", "glm-5.3", "glm-5.1", "kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "hy4-preview", "hy3")
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go/v1", "chat", "", "high", "openai", "glm-5.2")
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go/v1", "chat", "", "", "", "longcat-2.0", "mimo-v2.5", "mimo-v2.5-pro", "omen-alpha")
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go/v1", "chat", "enabled", "high", "deepseek", "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp")
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go", "anthropic", "adaptive", "", "", "minimax-m3", "minimax-m2.7", "qwen3.8-max", "qwen3.8-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.6-plus")
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go/v1", "responses", "", "low", "", "gpt-5.6-luna", "grok-4.6", "muse-spark-1.3-contributor", "muse-spark-1.2-contributor")
	// Custom DeepSeek protocol entries discussed in #9808, distinct from Go's
	// recommended Chat route. Availability is measured, never assumed.
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go", "anthropic", "adaptive", "high", "deepseek", "deepseek-v4-flash", "deepseek-v4-pro")
	add("go", "OPENCODE_GO_API_KEY", "https://opencode.ai/zen/go/v1", "responses", "", "high", "deepseek", "deepseek-v4-flash", "deepseek-v4-pro")
	// Flash/Pro already have full official recovery coverage; add vision model's
	// text/tool path here without claiming image understanding was tested.
	for _, proto := range []string{"chat", "anthropic", "responses"} {
		base := "https://api.deepseek.com"
		if proto == "anthropic" {
			base += "/anthropic"
		}
		add("deepseek", "DEEPSEEK_API_KEY", base, proto, "enabled", "high", "deepseek", "deepseek-v4-flash-vision-exp")
	}
	return out
}

func (tc multiProviderCase) upstream() string {
	suffix := map[string]string{"chat": "/chat/completions", "anthropic": "/v1/messages", "responses": "/responses"}[tc.protocol]
	return tc.base + suffix
}
func (tc multiProviderCase) new(t *testing.T, url, scenario string) provider.Provider {
	t.Helper()
	key := os.Getenv(tc.keyEnv)
	if effort := os.Getenv("REASONIX_LIVE_EFFORT"); effort != "" {
		tc.effort = effort
	}
	extra := map[string]any{"api_key_env": tc.keyEnv, "request_url": url, "reject_redirects": true}
	if scenario == "search" {
		extra["web_search"] = true
	}
	if scenario == "vision" {
		extra["vision"] = true
	}
	if tc.vendor == "longcat" || tc.vendor == "glm" {
		extra["auth_header"] = true
	}
	if tc.reasoning != "" {
		extra["reasoning_protocol"] = tc.reasoning
	}
	if tc.thinking != "" {
		extra["thinking"] = tc.thinking
	}
	if tc.effort != "" {
		extra["effort"] = tc.effort
	}
	if scenario == "disabled" {
		extra["thinking"] = "disabled"
		extra["effort"] = "disabled"
	}
	var p provider.Provider
	var err error
	switch tc.protocol {
	case "chat":
		p, err = openai.New(provider.Config{Name: "live-" + tc.vendor, BaseURL: tc.base, Model: tc.model, APIKey: key, Extra: extra})
	case "anthropic":
		p, err = anthropic.New(provider.Config{Name: "live-" + tc.vendor, BaseURL: tc.base, Model: tc.model, APIKey: key, Extra: extra})
	case "responses":
		effort := tc.effort
		if scenario == "disabled" {
			effort = "none"
		}
		p = responses.New(responses.Config{Name: "live-" + tc.vendor, BaseURL: tc.base, Model: tc.model, APIKey: key, KeyEnv: tc.keyEnv, Effort: effort, Mode: "stateless", MaxOutputTokens: 4096, RequestURL: url, Extra: extra, WebSearch: scenario == "search"})
	}
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := p.(interface{ CloseIdleConnections() }); ok {
		t.Cleanup(c.CloseIdleConnections)
	}
	return p
}

func TestLiveMultiProviderMatrix(t *testing.T) {
	scenarios := strings.Split(os.Getenv("REASONIX_LIVE_SCENARIOS"), ",")
	if len(scenarios) == 1 && scenarios[0] == "" {
		scenarios = []string{"baseline"}
	}
	blocked := map[string]string{}
	for _, tc := range multiProviderCases() {
		if os.Getenv(tc.keyEnv) == "" {
			continue
		}
		for _, scenario := range scenarios {
			t.Run(tc.vendor+"/"+tc.model+"/"+tc.protocol+"/"+scenario, func(t *testing.T) {
				if reason := blocked[tc.vendor+"/"+tc.protocol]; reason != "" {
					t.Skip("earlier credential/quota gate: " + reason)
				}
				runMultiProviderCase(t, tc, scenario, blocked)
			})
		}
	}
}

func runMultiProviderCase(t *testing.T, tc multiProviderCase, scenario string, blocked map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	proxy := &officialRecoveryProxy{protocol: tc.protocol, scenario: scenario, cancel: cancel, upstreamURL: tc.upstream()}

	srv := httptest.NewServer(proxy)
	defer srv.Close()
	p := tc.new(t, srv.URL, scenario)
	var executions atomic.Int32
	reg := tool.NewRegistry()
	reg.Add(liveRecoveryEchoTool{executions: &executions})
	sink := &recordSink{}
	system := "Call echo exactly once for each new user request, then report its fixed marker. Do not repeat completed work. Be concise."
	if os.Getenv("REASONIX_LIVE_PROMPT_PROFILE") == "action-evidence" {
		// A prompt-only experiment inspired by OpenCode's Kimi-specific action
		// instructions. This does not run OpenCode or change Reasonix defaults.
		system += " When the user requests a tool action, perform it using the provided tool instead of describing or simulating it. You cannot know this tool's result before executing it. Report only the actual returned result, and never invent a successful execution."
	}
	sess := NewSession(system)
	opts := Options{MaxSteps: 4, MaxOutputTokens: 4096, MissingReasoningWarnStateDir: t.TempDir()}
	a := New(p, reg, sess, opts, sink)
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	a.SetSessionPath(sessionPath)
	start := time.Now()
	err := a.Run(ctx, "Call echo exactly once, then report its result.")
	rounds := 1
	if scenario == "continuity" && err == nil {
		path := sessionPath
		lease, e := TryAcquireSessionLease(path)
		if e != nil {
			t.Fatal(e)
		}
		defer lease.Release()
		if e = sess.Save(path); e != nil {
			t.Fatal(e)
		}
		sess, e = LoadSession(path)
		if e != nil {
			t.Fatal(e)
		}
		a = New(p, reg, sess, opts, sink)
		a.SetSessionPath(path)
		for i := 2; i <= 3; i++ {
			rounds = i
			err = a.Run(ctx, fmt.Sprintf("New request %d: call echo once and report its result. Earlier requests are complete.", i))
			if err != nil {
				break
			}
		}
	}
	proxy.mu.Lock()
	requests, upstream, mutations := proxy.requests, proxy.upstream, proxy.mutations
	bodies := append([][]byte(nil), proxy.bodies...)
	statuses := append([]int(nil), proxy.statuses...)
	wireTools := append([]string(nil), proxy.wireTools...)
	wireStops := append([]string(nil), proxy.wireStops...)
	proxy.mu.Unlock()
	prompt, completion, cached, accounted := 0, 0, 0, 0
	unknown := false
	for _, e := range sink.kinds(event.Usage) {
		if u := e.Usage; u != nil {
			prompt += u.PromptTokens
			completion += u.CompletionTokens
			cached += u.CacheHitTokens
			accounted += u.RequestCount
			unknown = unknown || u.Unknown
		}
	}
	errorText := ""
	if err != nil {
		errorText = err.Error()
		failure := provider.ClassifyRecovery(err)
		if failure.Phase == "quota" {
			blocked[tc.vendor+"/"+tc.protocol] = failure.Phase
		}
	}
	reasoningBytes, thinkingBlocks, responseItems := 0, 0, 0
	for _, m := range sess.Snapshot() {
		reasoningBytes += len(m.ReasoningContent)
		thinkingBlocks += len(m.ThinkingBlocks)
		responseItems += len(m.ResponsesItems)
	}
	metric := map[string]any{"wire_tools": wireTools, "wire_stops": wireStops, "reasoning_bytes": reasoningBytes, "thinking_blocks": thinkingBlocks, "response_items": responseItems, "provider": tc.vendor, "model": tc.model, "protocol": tc.protocol, "scenario": scenario, "requests": requests, "upstream": upstream, "statuses": statuses, "mutations": mutations, "tools": executions.Load(), "rounds": rounds, "retries": len(sink.kinds(event.Retrying)), "prompt": prompt, "completion": completion, "cache_hit": cached, "accounted": accounted, "unknown_usage": unknown, "elapsed_ms": time.Since(start).Milliseconds(), "error": errorText}
	b, _ := json.Marshal(metric)
	t.Logf("METRIC %s", b)
	if scenario == "cancel_before_commit" {
		if !errors.Is(err, context.Canceled) || executions.Load() != 0 {
			t.Errorf("cancellation boundary: err=%v executions=%d", err, executions.Load())
		}
		return
	}
	if err != nil {
		t.Fatalf("live provider run: %v", err)
	}
	if executions.Load() != int32(rounds) {
		for _, m := range sess.Snapshot() {
			if m.Role == provider.RoleAssistant && m.Content != "" {
				text := m.Content
				if len(text) > 1024 {
					text = text[:1024]
				}
				t.Logf("visible_assistant=%q", text)
			}
		}
		t.Errorf("tool executions=%d want=%d", executions.Load(), rounds)
	}
	messages := sess.Snapshot()
	if len(messages) == 0 || strings.TrimSpace(messages[len(messages)-1].Content) == "" {
		t.Error("missing final content")
	}
	if accounted != requests {
		t.Errorf("usage request count=%d want=%d", accounted, requests)
	}
	if scenario == "server_replay_rejection" {
		rejected := false
		for _, status := range statuses {
			rejected = rejected || status == 400
		}
		if !rejected {
			t.Skip("upstream accepted modified replay; no rejection recovery exercised")
		}
	}
	if scenario == "cut_once" {
		if mutations != 1 || len(bodies) < 2 || !bytes.Equal(bodies[0], bodies[1]) {
			t.Error("cut fault or frozen retry invariant failed")
		}
	}
	if strings.HasPrefix(scenario, "missing") && mutations == 0 {
		t.Skip("endpoint produced no reasoning: missing-reasoning fault was not exercised")
	}
	if scenario == "continuity" {
		checkMultiProviderPrefix(t, tc.protocol, bodies)
	}
}

func checkMultiProviderPrefix(t *testing.T, protocol string, bodies [][]byte) {
	t.Helper()
	var previous map[string]json.RawMessage
	for n, body := range bodies {
		var current map[string]json.RawMessage
		if err := json.Unmarshal(body, &current); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			for _, field := range []string{"tools", "system", "model", "thinking", "reasoning", "output_config"} {
				if !bytes.Equal(previous[field], current[field]) {
					t.Errorf("request %d changed %s", n+1, field)
				}
			}
			field := "messages"
			if protocol == "responses" {
				field = "input"
			}
			var before, after []json.RawMessage
			if err := json.Unmarshal(previous[field], &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(current[field], &after); err != nil {
				t.Fatal(err)
			}
			if len(after) < len(before) {
				t.Errorf("request %d lost prefix", n+1)
			} else {
				for j := range before {
					if !bytes.Equal(before[j], after[j]) && !(protocol == "anthropic" && j == len(before)-1 && equalAfterMovingTailCacheMarker(before[j], after[j])) {
						t.Errorf("request %d changed history %d", n+1, j)
					}
				}
			}
		}
		previous = current
	}
}

func TestLiveMultiProviderWriteResume(t *testing.T) {
	for _, tc := range multiProviderCases() {
		if os.Getenv(tc.keyEnv) == "" {
			continue
		}
		t.Run(tc.vendor+"/"+tc.model+"/"+tc.protocol, func(t *testing.T) {
			p := tc.new(t, "", "baseline")
			runLiveWriteAfterEffectResume(t, p, tc.vendor+"/"+tc.model+"/"+tc.protocol)
		})
	}
}

// Anthropic moves the ephemeral breakpoint from the old request tail to the
// newly appended tail. Only that exact field on the old final content block
// may differ; tool output, reasoning, signatures and other blocks stay exact.
func equalAfterMovingTailCacheMarker(before, after json.RawMessage) bool {
	normalize := func(raw json.RawMessage) ([]byte, bool) {
		var message map[string]json.RawMessage
		if json.Unmarshal(raw, &message) != nil {
			return nil, false
		}
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(message["content"], &blocks) != nil || len(blocks) == 0 {
			return nil, false
		}
		last := blocks[len(blocks)-1]
		if marker, ok := last["cache_control"]; ok {
			var control map[string]string
			if json.Unmarshal(marker, &control) != nil || len(control) != 1 || control["type"] != "ephemeral" {
				return nil, false
			}
			delete(last, "cache_control")
		}
		content, err := json.Marshal(blocks)
		if err != nil {
			return nil, false
		}
		message["content"] = content
		out, err := json.Marshal(message)
		return out, err == nil
	}
	a, ok := normalize(before)
	b, ok2 := normalize(after)
	return ok && ok2 && bytes.Equal(a, b)
}

func TestLiveTailCacheComparisonPreservesProofAndToolBytes(t *testing.T) {
	before := json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"marker-alpha","cache_control":{"type":"ephemeral"}}]}`)
	after := json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"marker-alpha"}]}`)
	if !equalAfterMovingTailCacheMarker(before, after) {
		t.Fatal("valid tail marker move rejected")
	}
	for _, bad := range []string{
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-2","content":"marker-alpha"}]}`,
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"different"}]}`,
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"marker-alpha","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`,
	} {
		if equalAfterMovingTailCacheMarker(before, json.RawMessage(bad)) {
			t.Fatal("changed tool data or nonstandard marker accepted")
		}
	}
	thinking := json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"proof","signature":"sig","cache_control":{"type":"ephemeral"}}]}`)
	altered := json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"proof","signature":"changed"}]}`)
	if equalAfterMovingTailCacheMarker(thinking, altered) {
		t.Fatal("signature mutation hidden")
	}
}
