package agent

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// foldFixture is a transcript shaped like a real tool-driven run: fat tool-call
// arguments, reasoning on the assistant turns that carry them, and the decision
// receipts an approval-bearing session accumulates — the records that decide
// whether provider.ModelMessages can hand back its input or has to copy.
func foldFixture(turns int, cjk, receipts bool) []provider.Message {
	body := strings.Repeat("a plain english sentence about the work. ", 30)
	if cjk {
		body = strings.Repeat("这是一段关于当前工作的中文说明，包含具体的路径与结论。", 30)
	}
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: strings.Repeat("standing instruction. ", 200)}}
	for i := range turns {
		id := fmt.Sprintf("call_%d", i)
		asst := provider.Message{
			Role:             provider.RoleAssistant,
			Content:          body,
			ReasoningContent: strings.Repeat("weighing the options here. ", 60),
			ToolCalls: []provider.ToolCall{{
				ID: id, Name: "edit_file",
				Arguments: `{"path":"internal/x.go","old":"` + body + `","new":"` + body + `"}`,
			}},
		}
		if receipts && i%5 == 0 {
			asst.DecisionReceipts = []*provider.DecisionReceipt{{ID: "r", Kind: "approval", Outcome: "approved"}}
		}
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: "continue with the next step"},
			asst,
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "edit_file", Content: body},
		)
	}
	return msgs
}

func calibratedFoldAgent(msgs []provider.Message) *Agent {
	a := &Agent{agentConfig: agentConfig{contextWindow: 128_000, recentKeep: 2}}
	a.window().setPromptTokenCalibration(64_000, a.window().requestCalibrationShape(provider.Request{Messages: msgs}))
	return a
}

// remeasuredTailStart carries one measurement forward instead of re-measuring
// each candidate tail whole. That is only sound while a request's shape is the
// sum of its messages' shapes — for every suffix, on a transcript carrying the
// records ModelMessages strips, under every replay policy a provider declares.
func TestProjectedMessageShapesSumToTheWholeSliceMeasurement(t *testing.T) {
	msgs := append(foldFixture(40, true, true),
		provider.Message{Role: provider.RoleAssistant, Content: "local", LocalOnly: true},
		provider.Message{Role: provider.RoleAssistant, Content: "shown", ProviderContent: "sent to the provider instead"},
		provider.Message{Role: provider.RoleUser, Content: "raw", RawContent: "unprojected"},
	)
	for _, policy := range []provider.SharedWindowInputPolicy{
		{},
		{ReplaysOrdinaryReasoning: true},
		{ReplaysResponsesItems: true},
	} {
		for start := range len(msgs) + 1 {
			want := requestCalibrationShapeWithPolicy(
				provider.Request{Messages: provider.ModelMessages(msgs[start:])}, policy)
			var got requestCalibrationShape
			for _, m := range msgs[start:] {
				got = got.plus(projectedMessageCalibrationShape(m, policy))
			}
			if got != want {
				t.Fatalf("policy %+v start %d: summed %+v, whole-slice %+v", policy, start, got, want)
			}
		}
	}
}

// The same equivalence one level up, where it decides what a fold keeps: the
// incremental walk must stop on the message the rescanning walk stopped on.
func TestRemeasuredTailStartMatchesTheRescanningWalk(t *testing.T) {
	for _, cjk := range []bool{false, true} {
		msgs := foldFixture(200, cjk, true)
		a := calibratedFoldAgent(msgs)
		head := a.window().pinnedPrefixLen(msgs)
		budget := a.window().recentTailBudget()
		from := tailStart(msgs, head, budget, a.window().tokPerChar(), a.window().tailFloor())
		floor := max(head, len(msgs)-a.window().tailFloor())

		want := from
		for want < floor && a.window().estimatedPromptTokens(provider.ModelMessages(msgs[want:])) > budget {
			want++
			for want < floor && want < len(msgs) && msgs[want].Role == provider.RoleTool {
				want++
			}
		}
		if got := a.window().remeasuredTailStart(msgs, from, floor, budget); got != want {
			t.Fatalf("cjk=%v: incremental walk stopped at %d, rescanning walk at %d", cjk, got, want)
		}
	}
}
