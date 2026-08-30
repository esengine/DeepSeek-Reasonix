//go:build live

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

// TestLiveGoalBoundaryMatrix runs the three acceptance scenarios from the Goal
// boundary design against real OpenAI-compatible providers. Credentials are
// accepted only through the process environment; response text and keys are
// never logged or persisted. Deterministic unit tests cover exact internals.
func TestLiveGoalBoundaryMatrix(t *testing.T) {
	tests := []struct {
		name, keyEnv, baseURL, model string
		extra                        map[string]any
	}{
		{name: "deepseek", keyEnv: "DEEPSEEK_API_KEY", baseURL: "https://api.deepseek.com", model: "deepseek-v4-flash", extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled", "effort": "low"}},
		{name: "longcat", keyEnv: "LONGCAT_API_KEY", baseURL: "https://api.longcat.chat/openai/v1", model: "LongCat-2.0", extra: map[string]any{"thinking": "enabled", "effort": "enabled"}},
		{name: "zhipu-coding-plan", keyEnv: "GLM_PLAN_API_KEY", baseURL: "https://open.bigmodel.cn/api/coding/paas/v4", model: "glm-5.2", extra: map[string]any{"reasoning_protocol": "glm", "effort": "disabled"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := os.Getenv(tc.keyEnv)
			if key == "" {
				t.Skip(tc.keyEnv + " not set")
			}
			prov, err := openai.New(provider.Config{Name: tc.name, BaseURL: tc.baseURL, Model: tc.model, APIKey: key, Extra: tc.extra})
			if err != nil {
				t.Fatalf("create provider: %v", err)
			}
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}

			t.Run("normal-completion", func(t *testing.T) {
				metrics := &liveGoalMetrics{}
				a := newLiveGoalAgent(prov, tool.NewRegistry(), metrics)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				started := time.Now()
				if err := a.Run(ctx, "Return one short final sentence now without calling tools."); err != nil {
					t.Fatalf("normal completion: %v", err)
				}
				if metrics.requests.Load() < 1 {
					t.Fatal("normal completion emitted no provider request usage")
				}
				logLiveGoalMetrics(t, tc.model, "normal-completion", 1, metrics, "complete", time.Since(started))
			})

			t.Run("deterministic-stuck", func(t *testing.T) {
				const maxConformanceAttempts = 2
				for attempt := 1; attempt <= maxConformanceAttempts; attempt++ {
					metrics := &liveGoalMetrics{}
					var executions atomic.Int32
					reg := tool.NewRegistry()
					reg.Add(liveGoalFailureTool{executions: &executions})
					a := newLiveGoalAgent(prov, reg, metrics)
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
					ctx = WithDeliveryExecutionScope(ctx, DeliveryExecutionScope{ID: "live-goal-stuck", TaskText: "validate deterministic Goal stuck detection"})
					started := time.Now()
					err := a.Run(ctx, "This is a deterministic host-failure conformance probe requiring three failed tool rounds. Call live_goal_failure exactly once now. After each of the first two failures, call it exactly once again in the next assistant response. After the third failure, return a short summary. Do not switch tools, change arguments, or batch calls.")
					cancel()
					if err != nil {
						t.Fatalf("stuck guard paused Goal: attempt=%d err=%v executions=%d requests=%d", attempt, err, executions.Load(), metrics.requests.Load())
					}
					if executions.Load() == 3 {
						logLiveGoalMetrics(t, tc.model, "deterministic-stuck", 3, metrics, "redirect->complete", time.Since(started))
						return
					}
					t.Logf("provider conformance deviation: attempt=%d failure_executions=%d want=3", attempt, executions.Load())
				}
				t.Fatalf("provider failed three-round stuck conformance after %d attempts", maxConformanceAttempts)
			})

			t.Run("beyond-sixteen-rounds", func(t *testing.T) {
				metrics := &liveGoalMetrics{}
				var executions atomic.Int32
				reg := tool.NewRegistry()
				reg.Add(liveGoalMarkerTool{executions: &executions})
				a := newLiveGoalAgent(prov, reg, metrics)
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
				defer cancel()
				ctx = WithDeliveryExecutionScope(ctx, DeliveryExecutionScope{ID: "live-goal-continuous", TaskText: "validate continuous Goal execution"})
				started := time.Now()
				err := a.Run(ctx, "This is a 17-round tool conformance probe. Call live_goal_marker exactly once with index 1 now. After every accepted result, call it exactly once with the next integer index in your next assistant response. Never batch calls. After index 17 is accepted, return one short final sentence.")
				if err != nil {
					t.Fatalf("continuous Goal result: err=%v executions=%d requests=%d", err, executions.Load(), metrics.requests.Load())
				}
				if executions.Load() < 17 {
					t.Fatalf("tool executions = %d, want at least 17 continuous rounds", executions.Load())
				}
				if metrics.requests.Load() < 18 {
					t.Fatalf("provider requests = %d, want at least 17 work rounds and a final", metrics.requests.Load())
				}
				logLiveGoalMetrics(t, tc.model, "beyond-sixteen-rounds", 17, metrics, "complete", time.Since(started))
			})
		})
	}
}

type liveGoalMetrics struct {
	requests atomic.Int32
	tokens   atomic.Int64
}

func newLiveGoalAgent(prov provider.Provider, reg *tool.Registry, metrics *liveGoalMetrics) *Agent {
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Usage || e.Usage == nil {
			return
		}
		if e.Usage.RequestCount > 0 {
			metrics.requests.Add(int32(e.Usage.RequestCount))
		}
		tokens := e.Usage.TotalTokens
		if tokens <= 0 {
			tokens = e.Usage.PromptTokens + e.Usage.CompletionTokens
		}
		if tokens > 0 {
			metrics.tokens.Add(int64(tokens))
		}
	})
	return New(prov, reg, NewSession("You are a tool-call conformance test agent. Tool calls requested by the user are mandatory. Never substitute prose for a requested tool call. Call at most one tool per assistant response. Follow host finalization instructions exactly."), Options{Temperature: 0}, sink)
}

func logLiveGoalMetrics(t *testing.T, model, scenario string, rounds int, metrics *liveGoalMetrics, exit string, elapsed time.Duration) {
	t.Helper()
	t.Logf("model=%s scenario=%s rounds=%d requests=%d tokens=%d exit=%s latency=%s", model, scenario, rounds, metrics.requests.Load(), metrics.tokens.Load(), exit, elapsed.Round(time.Millisecond))
}

type liveGoalMarkerTool struct{ executions *atomic.Int32 }

func (t liveGoalMarkerTool) Name() string { return "live_goal_marker" }
func (t liveGoalMarkerTool) Description() string {
	return "Accept one numbered live-test round. Call exactly once per assistant response, using the next integer index requested by the prior result."
}
func (t liveGoalMarkerTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"index":{"type":"integer"}},"required":["index"],"additionalProperties":false}`)
}
func (t liveGoalMarkerTool) ReadOnly() bool { return true }
func (t liveGoalMarkerTool) Execute(context.Context, json.RawMessage) (string, error) {
	n := t.executions.Add(1)
	if n < 17 {
		return "accepted live round " + strconv.Itoa(int(n)) + "; in the next assistant response call live_goal_marker exactly once with index " + strconv.Itoa(int(n+1)) + "; do not write prose", nil
	}
	return "accepted live round 17; return one short final sentence now and do not call more tools", nil
}

type liveGoalFailureTool struct{ executions *atomic.Int32 }

func (t liveGoalFailureTool) Name() string { return "live_goal_failure" }
func (t liveGoalFailureTool) Description() string {
	return "Always return the same deterministic host failure. Call exactly once per assistant response when the user requests the failure probe."
}
func (t liveGoalFailureTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t liveGoalFailureTool) ReadOnly() bool { return true }
func (t liveGoalFailureTool) Execute(context.Context, json.RawMessage) (string, error) {
	n := t.executions.Add(1)
	if n < 3 {
		return "", errors.New("deterministic live host failure; call live_goal_failure exactly once again in the next assistant response and do not write prose")
	}
	return "", errors.New("deterministic live host failure number three reached; return one short summary now and do not call more tools")
}
