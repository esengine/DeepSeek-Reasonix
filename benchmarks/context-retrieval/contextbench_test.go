package main

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// The scorer decides what the pilot concludes, so it is held to the same rule
// the corpus is: judgements read structure. A model that says it will search
// has not searched; a recall call carrying a query has.

const scorerTarget = 42

func recallCall(id, args string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: id, Name: "recall", Arguments: args},
	}}
}

func toolResult(id, text string) provider.Message {
	return provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "recall", Content: text}
}

func hitBlock(positions ...int) string {
	var b strings.Builder
	b.WriteString("Folded context matching \"x\":\n")
	for _, p := range positions {
		fmt.Fprintf(&b, "\n#%d assistant_text\nsome snippet here\n", p)
	}
	return b.String()
}

func scorerTask() fixtureInstance {
	return fixtureInstance{Task: contextTask{ID: "t"}, AnswerMarkers: []string{"cobalt-lark-17"}}
}

func score(t *testing.T, msgs []provider.Message, final string) contextMetrics {
	t.Helper()
	m := scoreRun(msgs, scorerTask(), scorerTarget, "arm", false)
	m.scoreAnswer(final, scorerTask())
	return m
}

// The whole funnel, walked end to end.
func TestScorerReadsTheFullRetrievalChain(t *testing.T) {
	m := score(t, []provider.Message{
		recallCall("c1", `{"query":"retry boundary"}`),
		toolResult("c1", hitBlock(7, scorerTarget)),
		recallCall("c2", fmt.Sprintf(`{"positions":[%d]}`, scorerTarget)),
		toolResult("c2", "#42\n[assistant]\nthe ownership token is cobalt-lark-17"),
		{Role: provider.RoleAssistant, Content: "The token was cobalt-lark-17."},
	}, "The token was cobalt-lark-17.")

	if m.SearchCalls != 1 || m.ReadCalls != 1 {
		t.Errorf("calls = %d search / %d read, want 1/1", m.SearchCalls, m.ReadCalls)
	}
	if m.TargetSearchHits != 1 || m.FirstTargetRank != 2 {
		t.Errorf("target hit %d at rank %d, want 1 at rank 2", m.TargetSearchHits, m.FirstTargetRank)
	}
	if !m.TargetRead || !m.ReadAfterHit || m.RecallReadWithoutSearch {
		t.Errorf("read flags = read %v afterHit %v direct %v", m.TargetRead, m.ReadAfterHit, m.RecallReadWithoutSearch)
	}
	if !m.AnswerRecovered || m.FailureStage != stageRecovered {
		t.Errorf("stage = %q recovered=%v, want Recovered", m.FailureStage, m.AnswerRecovered)
	}
	if m.RecallReturnedTokens == 0 {
		t.Error("recall returned tokens were not counted")
	}
}

// An index arm's claim: the address was already on screen, so no search was
// needed. That is a different outcome from finding it, and gets its own stage.
func TestScorerSeparatesCueReadFromSearch(t *testing.T) {
	m := score(t, []provider.Message{
		recallCall("c1", fmt.Sprintf(`{"positions":[%d]}`, scorerTarget)),
		toolResult("c1", "#42\nthe ownership token is cobalt-lark-17"),
		{Role: provider.RoleAssistant, Content: "cobalt-lark-17"},
	}, "cobalt-lark-17")

	if m.SearchCalls != 0 || !m.RecallReadWithoutSearch {
		t.Errorf("search=%d direct=%v, want 0 and true", m.SearchCalls, m.RecallReadWithoutSearch)
	}
	if m.FailureStage != stageCueRead {
		t.Errorf("stage = %q, want %q", m.FailureStage, stageCueRead)
	}
}

// Every way the chain can break gets its own label, so a failed run says where
// rather than only that.
func TestScorerNamesWhereTheChainBroke(t *testing.T) {
	for _, tc := range []struct {
		name  string
		msgs  []provider.Message
		final string
		want  string
	}{
		{
			name:  "never touched recall",
			msgs:  []provider.Message{{Role: provider.RoleAssistant, Content: "I do not recall that."}},
			final: "I do not recall that.",
			want:  stageNoRetrieval,
		},
		{
			name: "searched and missed",
			msgs: []provider.Message{
				recallCall("c1", `{"query":"unrelated words"}`),
				toolResult("c1", hitBlock(3, 9)),
			},
			final: "not found",
			want:  stageSearchMiss,
		},
		{
			name: "found it and never opened it",
			msgs: []provider.Message{
				recallCall("c1", `{"query":"retry boundary"}`),
				toolResult("c1", hitBlock(scorerTarget)),
			},
			final: "probably something about retries",
			want:  stageHitNotRead,
		},
		{
			name: "read it and answered wrong",
			msgs: []provider.Message{
				recallCall("c1", fmt.Sprintf(`{"positions":[%d]}`, scorerTarget)),
				toolResult("c1", "#42\nthe token is elsewhere"),
			},
			final: "I think it was amber-9",
			want:  stageAnswerWrong,
		},
		{
			name: "read the wrong position",
			msgs: []provider.Message{
				recallCall("c1", `{"positions":[7]}`),
				toolResult("c1", "#7\nnothing useful"),
			},
			final: "unclear",
			want:  stageReadMissed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := score(t, tc.msgs, tc.final).FailureStage; got != tc.want {
				t.Errorf("stage = %q, want %q", got, tc.want)
			}
		})
	}
}

// The answer lives only in folded history, so reaching for the workspace is a
// strategy change worth seeing — including when it happens to work.
func TestScorerRecordsWorkspaceEscapes(t *testing.T) {
	m := score(t, []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g1", Name: "bash", Arguments: `{"command":"rg cobalt-lark"}`},
			{ID: "g2", Name: "read_file", Arguments: `{"path":"notes.md"}`},
		}},
		{Role: provider.RoleAssistant, Content: "cobalt-lark-17"},
	}, "cobalt-lark-17")

	if len(m.UnexpectedWorkTools) != 2 {
		t.Fatalf("escapes = %v, want bash and read_file", m.UnexpectedWorkTools)
	}
	if m.SearchCalls != 0 || m.ReadCalls != 0 {
		t.Errorf("a workspace tool was counted as recall: search=%d read=%d", m.SearchCalls, m.ReadCalls)
	}
	// It answered, so the run is Recovered — the escape is reported beside the
	// outcome rather than overriding it.
	if !m.AnswerRecovered {
		t.Error("a correct answer was not scored as recovered")
	}
}

// Narration is not action.
func TestScorerIgnoresWhatTheModelSaysItWillDo(t *testing.T) {
	m := score(t, []provider.Message{
		{Role: provider.RoleAssistant, Content: "Let me search the folded context with recall for the retry boundary."},
	}, "Let me search the folded context with recall for the retry boundary.")
	if m.SearchCalls != 0 || m.FailureStage != stageNoRetrieval {
		t.Errorf("prose was scored as a search: calls=%d stage=%q", m.SearchCalls, m.FailureStage)
	}
}

// Every marker must land, so a partially right answer is not a recovery.
func TestScorerRequiresEveryMarker(t *testing.T) {
	task := fixtureInstance{Task: contextTask{ID: "t"}, AnswerMarkers: []string{"comet-42", "per-lineage"}}
	m := scoreRun(nil, task, scorerTarget, "arm", false)
	m.scoreAnswer("the salt was comet-42", task)
	if m.AnswerRecovered {
		t.Error("a half answer scored as recovered")
	}
	if len(m.MissingMarkers) != 1 || m.MissingMarkers[0] != "per-lineage" {
		t.Errorf("missing = %v, want per-lineage", m.MissingMarkers)
	}
}

// An answer reachable through anything but recall invalidates the run. The
// workspace is isolated so this should never fire; asserting it beats trusting
// it, and a tripped run leaves the statistics rather than inflating one.
func TestScorerInvalidatesRunsThatLeakedThroughAnotherTool(t *testing.T) {
	m := score(t, []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g1", Name: "bash", Arguments: `{"command":"rg -r ."}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "g1", Name: "bash", Content: "corpus.go: the token is cobalt-lark-17"},
		{Role: provider.RoleAssistant, Content: "cobalt-lark-17"},
	}, "cobalt-lark-17")

	if !m.Contaminated || m.LeakedVia != "bash" || m.LeakedMarker != "cobalt-lark-17" {
		t.Fatalf("leak not caught: contaminated=%v via=%q marker=%q", m.Contaminated, m.LeakedVia, m.LeakedMarker)
	}
	if m.FailureStage != stageContaminated {
		t.Errorf("stage = %q, want %q even though every marker matched", m.FailureStage, stageContaminated)
	}
	f := summarize("arm", []contextMetrics{m})
	if f.Scored != 0 || f.Answered != 0 || f.Contaminated != 1 {
		t.Errorf("a contaminated run entered the statistics: %+v", f)
	}
}

// Where an escape happened matters: before any recall is a different strategy
// from after one came back short.
func TestScorerPlacesEscapesRelativeToRecall(t *testing.T) {
	m := score(t, []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g1", Name: "bash", Arguments: `{"command":"ls"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "g1", Name: "bash", Content: "README.md"},
		recallCall("c1", `{"query":"retry boundary"}`),
		toolResult("c1", hitBlock(scorerTarget)),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g2", Name: "read_file", Arguments: `{"path":"README.md"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "g2", Name: "read_file", Content: "nothing here"},
	}, "unclear")

	if m.EscapeCalls != 2 || m.EscapeBeforeFirstRecall != 1 || m.EscapeAfterFirstRecall != 1 {
		t.Errorf("escapes = %d (%d before, %d after), want 2 (1, 1)",
			m.EscapeCalls, m.EscapeBeforeFirstRecall, m.EscapeAfterFirstRecall)
	}
}

// Routing is judged by round. Two calls in one round are a fan-out, and
// reporting that as a preference would invent a decision the model never made.
func TestRoutingIsJudgedByRoundNotEventOrder(t *testing.T) {
	parallel := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g1", Name: "bash", Arguments: `{"command":"ls"}`},
			{ID: "c1", Name: "recall", Arguments: `{"query":"retry boundary"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "g1", Name: "bash", Content: "README.md"},
		toolResult("c1", hitBlock(scorerTarget)),
	}
	if got := score(t, parallel, "unclear").Routing; got != routeParallel {
		t.Errorf("routing = %q for one round holding both, want %q", got, routeParallel)
	}

	workspaceFirst := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g1", Name: "bash", Arguments: `{"command":"ls"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "g1", Name: "bash", Content: "README.md"},
		recallCall("c1", `{"query":"retry boundary"}`),
		toolResult("c1", hitBlock(scorerTarget)),
	}
	if got := score(t, workspaceFirst, "unclear").Routing; got != routeWorkspaceFirst {
		t.Errorf("routing = %q for workspace in an earlier round, want %q", got, routeWorkspaceFirst)
	}

	memoryFirst := []provider.Message{
		recallCall("c1", `{"query":"retry boundary"}`),
		toolResult("c1", hitBlock(scorerTarget)),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "g1", Name: "bash", Arguments: `{"command":"ls"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "g1", Name: "bash", Content: "README.md"},
	}
	if got := score(t, memoryFirst, "unclear").Routing; got != routeMemoryFirst {
		t.Errorf("routing = %q for recall in an earlier round, want %q", got, routeMemoryFirst)
	}

	if got := score(t, nil, "no idea").Routing; got != routeNeither {
		t.Errorf("routing = %q with no tools at all, want %q", got, routeNeither)
	}
}

// Markers matching proves the right fact appears somewhere. An answer that
// hedges between two candidates contains it and has not submitted it.
func TestScorerFlagsAnAnswerThatNamesTwoCandidates(t *testing.T) {
	inst := fixtureInstance{
		Task:          contextTask{ID: "t", Vars: []varSpec{code("marker", "fence")}},
		AnswerMarkers: []string{"fence-qmwnv"},
	}
	m := scoreRun(nil, inst, 0, "arm", false)
	m.scoreAnswer("It was fence-qmwnv, though it may have been fence-abcde.", inst)
	if !m.AnswerRecovered {
		t.Error("the marker is present; recovery should still read true")
	}
	if !m.AnswerAmbiguous || m.AmbiguousVar != "marker" {
		t.Errorf("ambiguity not flagged: %v %q", m.AnswerAmbiguous, m.AmbiguousVar)
	}

	clean := scoreRun(nil, inst, 0, "arm", false)
	clean.scoreAnswer("The marker was fence-qmwnv.", inst)
	if clean.AnswerAmbiguous {
		t.Error("a single candidate was called ambiguous")
	}
}

// The index arm's claim is narrow: the cue was addressed, the first memory
// action was a positions read, and it covered the target. A positions call
// after a search located the target credits the index for the search's work.
func TestCueDirectReadRequiresTheCueAndTheFirstAction(t *testing.T) {
	direct := []provider.Message{
		recallCall("c1", fmt.Sprintf(`{"positions":[%d]}`, scorerTarget)),
		toolResult("c1", "#42\nthe token is cobalt-lark-17"),
	}
	m := scoreRun(direct, scorerTask(), scorerTarget, "arm", true)
	if !m.CueDirectRead {
		t.Error("a first-action read with the cue visible was not credited")
	}

	// Same read, but no cue was on screen to give the address.
	if scoreRun(direct, scorerTask(), scorerTarget, "arm", false).CueDirectRead {
		t.Error("a read was credited to a cue that was not visible")
	}

	// Same read, but a search found the address first.
	afterSearch := append([]provider.Message{
		recallCall("c0", `{"query":"retry boundary"}`),
		toolResult("c0", hitBlock(scorerTarget)),
	}, direct...)
	if scoreRun(afterSearch, scorerTask(), scorerTarget, "arm", true).CueDirectRead {
		t.Error("a read after a search was credited to the index")
	}
}
