//go:build live

package agent

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// The proxy corrupts only the second outbound replay request. Rejections are
// real upstream responses. Explicit recovery is measured separately from
// automatic success; it never converts a stopped initial turn to a pass.
func TestLiveManualProtocolRecovery(t *testing.T) {
	cases := multiProviderCases()
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		for _, protocol := range []string{"chat", "anthropic", "responses"} {
			base := "https://api.deepseek.com"
			if protocol == "anthropic" {
				base += "/anthropic"
			}
			cases = append(cases, multiProviderCase{"deepseek", "DEEPSEEK_API_KEY", model, protocol, base, "enabled", "high", "deepseek"})
		}
	}
	executed := 0
	for _, tc := range cases {
		if os.Getenv(tc.keyEnv) == "" || !strings.HasPrefix(tc.model, "deepseek-v4-") || strings.Contains(tc.model, "vision") {
			continue
		}
		executed++
		t.Run(tc.vendor+"/"+tc.model+"/"+tc.protocol, func(t *testing.T) {
			proxy := &officialRecoveryProxy{protocol: tc.protocol, scenario: "server_replay_rejection", upstreamURL: tc.upstream()}
			srv := httptest.NewServer(proxy)
			defer srv.Close()
			p := tc.new(t, srv.URL, "server_replay_rejection")
			var executions atomic.Int32
			reg := tool.NewRegistry()
			reg.Add(liveRecoveryEchoTool{executions: &executions})
			sink := &recordSink{}
			session := NewSession("Call echo once, report only its real marker. Preserve completed work; never repeat completed tool calls when continuing after an error.")
			a := New(p, reg, session, Options{MaxSteps: 4, MaxOutputTokens: 4096, MissingReasoningWarnStateDir: t.TempDir()}, sink)
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()
			first := a.Run(ctx, "Call echo once and report the result.")
			explicit := false
			var recovered error
			if pending := a.PendingProtocolRecovery(); pending != nil {
				explicit = true
				recovered = a.Run(WithInputMessageOrigin(WithProtocolRecovery(ctx, pending.ID), provider.MessageOriginHost), "Continue from valid history; report the completed echo result without repeating it.")
				if a.PendingProtocolRecovery() != nil {
					t.Fatal("repair budget renewed")
				}
			}
			proxy.mu.Lock()
			statuses := append([]int(nil), proxy.statuses...)
			proxy.mu.Unlock()
			requests, prompt, output := 0, 0, 0
			for _, e := range sink.kinds(event.Usage) {
				if e.Usage != nil {
					requests += e.Usage.RequestCount
					prompt += e.Usage.PromptTokens
					output += e.Usage.CompletionTokens
				}
			}
			t.Logf("initial_success=%t explicit_action=%t recovery_success=%t tools=%d requests=%d prompt=%d output=%d statuses=%v", first == nil, explicit, explicit && recovered == nil, executions.Load(), requests, prompt, output, statuses)
			if first != nil && !explicit {
				t.Fatalf("initial error without eligible recovery: %v", first)
			}
			if recovered != nil {
				t.Fatal(recovered)
			}
			if executions.Load() != 1 {
				t.Fatal("completed tool was repeated or never executed")
			}
		})
	}
	if executed == 0 {
		t.Skip("no eligible credential or model")
	}
}
