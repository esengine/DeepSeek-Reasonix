package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

type coordinatorBudgetRead struct{ calls int }

func (*coordinatorBudgetRead) Name() string        { return "read_fixture" }
func (*coordinatorBudgetRead) Description() string { return "Read a fixture" }
func (*coordinatorBudgetRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (*coordinatorBudgetRead) ReadOnly() bool { return true }
func (r *coordinatorBudgetRead) Execute(context.Context, json.RawMessage) (string, error) {
	r.calls++
	return "fixture", nil
}

func coordinatorBudgetRounds() [][]provider.Chunk {
	var rounds [][]provider.Chunk
	for i := range 3 {
		rounds = append(rounds, []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprint(i), Name: "read_fixture", Arguments: "{}"}}, {Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}}, {Type: provider.ChunkDone}})
	}
	return append(rounds, []provider.Chunk{{Type: provider.ChunkText, Text: "Complete."}, {Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}}, {Type: provider.ChunkDone}})
}

func TestCoordinatorRootBudgetRoutes(t *testing.T) {
	for _, mode := range []string{"raw", "tool_planner", "failure_fallback", "plan_only_continuation", "approval_declined_continuation", "approval_allowed"} {
		t.Run(mode, func(t *testing.T) {
			planner := &mockProvider{name: "planner", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Read the fixture."}, {Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}}, {Type: provider.ChunkDone}}}
			if mode == "failure_fallback" {
				planner.chunks[2] = provider.Chunk{Type: provider.ChunkError, Err: errors.New("fixture failure")}
			}
			exec := &mockProvider{name: "executor", streams: coordinatorBudgetRounds()}
			reads := &coordinatorBudgetRead{}
			registry := tool.NewRegistry()
			registry.Add(reads)
			executor := agent.New(exec, registry, sessionstore.NewSession("executor"), agent.Options{TaskBudget: agent.TaskBudget{Tokens: 2000}}, event.Discard)
			var plannerTools *tool.Registry
			if mode == "tool_planner" {
				plannerTools = tool.NewRegistry()
			}
			route := agent.PlannerRoutePlanAndExecute
			if mode == "plan_only_continuation" {
				route = agent.PlannerRoutePlanOnly
			}
			if mode == "approval_declined_continuation" || mode == "approval_allowed" {
				route = agent.PlannerRoutePlanForApproval
			}
			coord := NewCoordinatorWithPlannerPolicy(planner, sessionstore.NewSession("planner"), nil, plannerTools, agent.Options{}, executor, 0, event.Discard, func(context.Context, string) agent.PlannerDecision {
				return agent.PlannerDecision{Route: route, Depth: agent.PlannerDepthFull}
			})
			coord.SetPlannerPlanApprover(&coordinatorApprovalGate{allow: mode == "approval_allowed"})
			ctx := agent.WithDeliveryExecutionScope(t.Context(), agent.DeliveryExecutionScope{ID: "shared-root", TaskText: "read the fixture"})
			err := coord.Run(ctx, "read the fixture")
			if mode == "plan_only_continuation" || mode == "approval_declined_continuation" {
				if err != nil || reads.calls != 0 {
					t.Fatalf("unapproved execution calls=%d err=%v", reads.calls, err)
				}
				route = agent.PlannerRouteExecutorOnly
				err = coord.Run(ctx, "continue")
			}
			wantRequests := 2
			if mode == "raw" || mode == "tool_planner" || mode == "approval_allowed" {
				wantRequests = 1
			}
			if p, ok := agent.InspectRunPause(err); !ok || p.Key != "token" || reads.calls != 1 || len(exec.requests) != wantRequests {
				t.Fatalf("read=%d HTTP=%d pause=%+v err=%v", reads.calls, len(exec.requests), p, err)
			}
		})
	}
}
