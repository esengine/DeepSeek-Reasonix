//go:build live

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// Six consecutive user turns reuse real reasoning and tool history. A save/load
// halfway through verifies that restart does not change healthy provider bytes.
func TestLiveOfficialConversationContinuity(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		for _, protocol := range []string{"chat", "responses", "anthropic"} {
			t.Run(model+"/"+protocol, func(t *testing.T) {
				proxy := &officialRecoveryProxy{protocol: protocol, scenario: "continuity"}
				srv := httptest.NewServer(proxy)
				defer srv.Close()
				p := officialMatrixProvider(t, key, model, protocol, "high", srv.URL)
				reg := tool.NewRegistry()
				var executions atomic.Int32
				reg.Add(liveRecoveryEchoTool{executions: &executions})
				sess := NewSession("Call echo exactly once for each new user request, then report its fixed marker. " + strings.Repeat("Keep completed work and history intact. ", 100))
				sink := &recordSink{}
				opts := Options{MaxSteps: 4, MaxOutputTokens: 2048, MissingReasoningWarnStateDir: t.TempDir()}
				a := New(p, reg, sess, opts, sink)
				path := filepath.Join(t.TempDir(), "session.jsonl")
				lease, err := TryAcquireSessionLease(path)
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Release()
				for round := 1; round <= 6; round++ {
					ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
					err := a.Run(ctx, fmt.Sprintf("New request %d: call echo exactly once and report the marker. Earlier requests are complete.", round))
					cancel()
					if err != nil {
						t.Fatalf("round %d: %v", round, err)
					}
					if executions.Load() != int32(round) {
						t.Fatalf("round=%d total executions=%d", round, executions.Load())
					}
					if round == 3 {
						if err := sess.Save(path); err != nil {
							t.Fatal(err)
						}
						sess, err = LoadSession(path)
						if err != nil {
							t.Fatal(err)
						}
						a = New(p, reg, sess, opts, sink)
					}
				}
				proxy.mu.Lock()
				defer proxy.mu.Unlock()
				if proxy.requests != 12 {
					t.Fatalf("requests=%d want 12", proxy.requests)
				}
				var previous map[string]json.RawMessage
				for n, body := range proxy.bodies {
					var current map[string]json.RawMessage
					if err := json.Unmarshal(body, &current); err != nil {
						t.Fatal(err)
					}
					if n > 0 {
						for _, field := range []string{"tools", "system", "model", "thinking", "reasoning", "output_config"} {
							if !bytes.Equal(previous[field], current[field]) {
								t.Fatalf("request %d changed %s", n+1, field)
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
							t.Fatalf("request %d lost prefix", n+1)
						}
						for j := range before {
							if !bytes.Equal(before[j], after[j]) {
								t.Fatalf("request %d changed historical message %d", n+1, j)
							}
						}
					}
					previous = current
				}
				prompt, completion, hit, requests := 0, 0, 0, 0
				for _, e := range sink.kinds(event.Usage) {
					if u := e.Usage; u != nil {
						prompt += u.PromptTokens
						completion += u.CompletionTokens
						hit += u.CacheHitTokens
						requests += u.RequestCount
					}
				}
				t.Logf("protocol=%s rounds=6 restarts=1 http_attempts=%d tool_executions=%d retries=%d prompt=%d completion=%d cache_hit=%d", protocol, requests, executions.Load(), len(sink.kinds(event.Retrying)), prompt, completion, hit)
			})
		}
	}
}
