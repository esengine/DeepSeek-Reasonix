package control

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/evidence"
)

func TestDelegatedUsageKeepsGoalOwner(t *testing.T) {
	for _, mode := range []string{"chat_to_goal", "replaced_goal", "cleared_goal", "resumed_goal", "same_goal_next_turn", "between_turns", "nested_after_replacement"} {
		t.Run(mode, func(t *testing.T) {
			var forwarded []event.Event
			tee := NewGoalUsageTee(event.FuncSink(func(e event.Event) { forwarded = append(forwarded, e) })).(*goalUsageTee)
			g := &goalMachine{}
			bind := func(name string) *goalTurnRecorder {
				g.set(name, "", evidence.VerificationContract{}, nil)
				r := g.newTurnRecorder(g.scopeID, g.continuationEpoch)
				tee.setActiveRecorder(r)
				return r
			}
			var first *goalTurnRecorder
			if mode != "chat_to_goal" {
				first = bind("original")
			}
			capture := func(sink event.Sink) event.Sink {
				_, child, _, ok := agent.CallContext(agent.WithCallContext(t.Context(), "child", sink, nil, false))
				if !ok {
					t.Fatal("missing call context")
				}
				return child
			}
			child := capture(event.NewCostQuoteSink(tee, nil))
			want := 0
			switch mode {
			case "chat_to_goal", "replaced_goal", "nested_after_replacement":
				bind("replacement")
			case "cleared_goal":
				g.set("", "", evidence.VerificationContract{}, nil)
				tee.setActiveRecorder(nil)
			case "resumed_goal":
				g.pauseFor(stopCauseManual, "paused", nil)
				if _, _, _, resumed := g.resume(nil); !resumed {
					t.Fatal("goal did not resume")
				}
				tee.setActiveRecorder(g.newTurnRecorder(g.scopeID, g.continuationEpoch))
			case "same_goal_next_turn":
				tee.setActiveRecorder(g.newTurnRecorder(g.scopeID, g.continuationEpoch))
				want = 1100
			case "between_turns":
				tee.setActiveRecorder(nil)
				want = 1100
			}
			if mode == "nested_after_replacement" {
				child = capture(child)
			}
			usage := &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, RequestCount: 1}
			child.Emit(event.Event{Kind: event.Usage, Usage: usage, UsageSource: event.UsageSourceSubagent})
			if g.tokensUsed != want {
				t.Fatalf("goal tokens=%d want %d", g.tokensUsed, want)
			}
			if first != nil && first.usageTokens() != want {
				t.Fatalf("original recorder tokens=%d want %d", first.usageTokens(), want)
			}
			if want > 0 && tee.activeRecorder() != nil && tee.activeRecorder().usageTokens() != 0 {
				t.Fatal("late usage charged the next turn's recorder")
			}
			if len(forwarded) != 1 || forwarded[0].Usage != usage || tee.tally.usage.PromptTokens != 1000 {
				t.Fatal("usage forwarding or session tally changed")
			}
			child.Emit(event.Event{Kind: event.Usage, Usage: usage, UsageSource: event.UsageSourceTitle})
			if g.tokensUsed != want || tee.tally.usage.PromptTokens != 1000 {
				t.Fatal("title charged to goal or session tally")
			}
		})
	}
}
