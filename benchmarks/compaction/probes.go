package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// probeAnswerContract rides with every probe question. A context full of tool
// calls invites the model to answer with another one, which scores as a lost
// fact when it is really a harness artifact — both arms get the same nudge.
const probeAnswerContract = "Answer using only the conversation above. Reply with the answer itself in plain text: no tool calls, no tool-call syntax, no explanation."

// noAnswerMarker and toolCallMarker label a reply that never answered, so the
// report can separate what compaction lost from what the harness failed to ask.
const (
	noAnswerMarker  = "<no answer"
	toolCallMarker  = "DSML"
	toolCallInvalid = "<tool-call syntax instead of an answer>"
)

// invalidAnswer reports whether a reply failed to answer at all, rather than
// answering wrongly. These are excluded from the survival rate and counted.
func invalidAnswer(s string) bool {
	return strings.HasPrefix(s, noAnswerMarker) || s == toolCallInvalid
}

// A probe is a fact planted in history and a question only that fact answers.
// The question is asked against the compacted context, so a wrong answer means
// compaction lost the fact — not that the model is weak.
type probe struct {
	class   string
	plantAt int
	plant   func(*agent.Session)
	// later plants more of the same fact in later generations, so a decision
	// that changes across folds is tested the way it actually happens: each
	// revision lands on the far side of a compaction, not next to the last one.
	later    map[int]func(*agent.Session)
	question string
	want     []string // answer must contain one of these, lowercased
	reject   []string // ...and none of these: the pre-correction answer
}

func userTurn(text string) func(*agent.Session) {
	return func(s *agent.Session) { s.Add(provider.Message{Role: provider.RoleUser, Content: text}) }
}

func toolRound(id, name, args, result string) func(*agent.Session) {
	return func(s *agent.Session) {
		s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: name, Arguments: args}}})
		s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: name, Content: result})
	}
}

// failedToolRound is a tool round the host recorded as failed, the way a real
// non-zero bash run arrives. Without the execution record the keep policy sees
// only text, which is exactly the gap the buried-evidence probe measures.
func failedToolRound(id, name, args, result string, code int) func(*agent.Session) {
	return func(s *agent.Session) {
		exit := code
		s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: name, Arguments: args}}})
		s.Add(provider.Message{
			Role: provider.RoleTool, ToolCallID: id, Name: name, Content: result,
			ToolExecution: &provider.ToolExecution{
				Kind:     "shell",
				State:    tool.ShellStateFailed,
				ExitCode: &exit,
			},
		})
	}
}

func seq(fns ...func(*agent.Session)) func(*agent.Session) {
	return func(s *agent.Session) {
		for _, fn := range fns {
			fn(s)
		}
	}
}

// probeSuite covers what a coding agent must not lose across a fold. The
// freshness and correction probes are the ones summaries classically get wrong:
// both have a plausible stale answer that reads as correct.
func probeSuite() []probe {
	suite := append(factProbes(), revisionProbes()...)
	return append(suite, snipProbes()...)
}

// buriedTestLog is a `go test -v` log whose only failure detail sits in the
// middle. Geometric snipping keeps the first 80 and last 12 lines, so this is
// precisely the shape it drops while reporting a plausible-looking remainder.
// It names a package no other probe touches: a run covering config/ would be a
// truthful "yes" for verification-freshness and score that probe as lost.
func buriedTestLog() string {
	var b strings.Builder
	pass := func(i int) {
		fmt.Fprintf(&b, "=== RUN   TestEmitterCase%03d\n--- PASS: TestEmitterCase%03d (0.00s)\n", i, i)
	}
	for i := range 120 {
		pass(i)
	}
	b.WriteString("=== RUN   TestEmitterQuoting\n")
	b.WriteString("    emitter_test.go:412: assertion failed: expected beta-7d21, got gamma-4a88\n")
	b.WriteString("--- FAIL: TestEmitterQuoting (0.01s)\n")
	for i := 120; i < 200; i++ {
		pass(i)
	}
	b.WriteString("FAIL\nexit status 1\nFAIL\treasonix/emitter\t0.234s\n")
	return b.String()
}

// snipProbes plant a fact in the middle of a long tool result, out of reach of
// the head/tail geometry. They score the maintenance pass rather than the fold:
// every other probe in the suite is short enough that snipping cannot touch it.
func snipProbes() []probe {
	return []probe{
		{
			class:    "buried-evidence",
			plantAt:  1,
			plant:    failedToolRound("bt1", "bash", `{"cmd":"go test ./emitter -v"}`, buriedTestLog(), 1),
			question: "In the failed test, what value did the assertion expect? Reply with just the value.",
			want:     []string{"beta-7d21"},
			reject:   []string{"gamma-4a88"},
		},
	}
}

func factProbes() []probe {
	return []probe{
		{
			class:    "user-constraint",
			plantAt:  0,
			plant:    userTurn("Hard rule for this whole task: never modify config/schema.sql."),
			question: "Are you allowed to modify config/schema.sql? Answer yes or no.",
			want:     []string{"no"},
			reject:   []string{"yes"},
		},
		{
			class:   "correction",
			plantAt: 0,
			plant: seq(
				userTurn("Indent the generated config with tabs."),
				userTurn("Correction: indent the generated config with spaces, not tabs. This supersedes what I said before."),
			),
			question: "Should the generated config be indented with tabs or with spaces? Answer with one word.",
			want:     []string{"spaces", "space"},
			reject:   []string{"tabs", "tab"},
		},
		{
			class:    "exact-identifier",
			plantAt:  0,
			plant:    userTurn("Track this work under ticket RX-4821; put that id in the commit message."),
			question: "What is the ticket id for this work? Reply with just the id.",
			want:     []string{"rx-4821"},
		},
		{
			class:    "objective",
			plantAt:  0,
			plant:    userTurn("To be clear, the objective is the config round-trip formatting bug, not performance."),
			question: "In a few words, what is the current objective?",
			want:     []string{"round-trip", "round trip", "roundtrip", "formatting"},
		},
		{
			class:   "pending-requirement",
			plantAt: 1,
			plant: seq(
				userTurn("Two requirements: R1 preserve unknown keys, R2 keep quoted values quoted."),
				toolRound("r1", "bash", `{"cmd":"go test ./config -run TestUnknownKeys"}`, "ok\nPASS: TestUnknownKeys (R1 satisfied)"),
			),
			question: "Of requirements R1 and R2, which one is still not satisfied? Reply with just R1 or R2.",
			want:     []string{"r2"},
			reject:   []string{"r1"},
		},
		{
			class:   "verification-freshness",
			plantAt: 1,
			plant: seq(
				toolRound("v1", "bash", `{"cmd":"go test ./config -run TestRoundTrip"}`, "ok  config\tPASS: TestRoundTrip"),
				toolRound("w1", "write_file", `{"path":"config/format.go"}`, "wrote config/format.go (42 lines changed)"),
				userTurn("Note that config/format.go changed after that test run."),
			),
			question: "Has TestRoundTrip been run again since config/format.go was last edited? Answer yes or no.",
			want:     []string{"no"},
			reject:   []string{"yes"},
		},
		{
			class:   "negative-evidence",
			plantAt: 1,
			plant: seq(
				userTurn("We suspected parser normalization was the root cause."),
				toolRound("n1", "bash", `{"cmd":"go test ./config -run TestParserNormalization"}`, "PASS — parser normalization is NOT the root cause; ruled out."),
			),
			question: "Is parser normalization the root cause of the bug? Answer yes or no.",
			want:     []string{"no"},
			reject:   []string{"yes"},
		},
		{
			class:   "tool-outcome",
			plantAt: 2,
			plant: seq(
				toolRound("b1", "bash", `{"cmd":"go vet ./..."}`, "config/format.go:88: printf: non-constant format string\nexit status 1"),
				userTurn("Leave that vet warning for now; we will fix it at the end."),
			),
			question: "Did `go vet ./...` pass the last time it ran? Answer yes or no.",
			want:     []string{"no"},
			reject:   []string{"yes"},
		},
		{
			class:    "code-fact",
			plantAt:  2,
			plant:    toolRound("c1", "read_file", `{"path":"config/save.go"}`, "// Config.Save intentionally preserves unknown keys so plugins round-trip through this path.\nfunc (c *Config) Save() error { /* ... */ }"),
			question: "Does Config.Save preserve unknown keys? Answer yes or no.",
			want:     []string{"yes"},
			reject:   []string{"no"},
		},
		{
			class:   "chronology",
			plantAt: 2,
			plant: seq(
				toolRound("o1", "write_file", `{"path":"config/parser.go"}`, "wrote config/parser.go"),
				toolRound("o2", "write_file", `{"path":"config/format.go"}`, "wrote config/format.go"),
			),
			question: "Was config/parser.go edited before or after config/format.go? Reply with just: before, or after.",
			want:     []string{"before"},
			reject:   []string{"after"},
		},
	}
}

// revisionProbes state a fact and then change it in a later generation. The
// revision lands on the far side of a fold, so carrying it forward means
// superseding the digest's own earlier claim rather than a neighbouring turn.
func revisionProbes() []probe {
	return []probe{
		{
			class:    "late-constraint",
			plantAt:  3,
			plant:    userTurn("New hard rule from here on: never edit anything under internal/store/."),
			question: "Are you allowed to edit files under internal/store/? Answer yes or no.",
			want:     []string{"no"},
			reject:   []string{"yes"},
		},
		{
			class:   "reversal-chain",
			plantAt: 0,
			plant:   userTurn("Use PostgreSQL for the datastore."),
			later: map[int]func(*agent.Session){
				1: userTurn("Change of plan: drop PostgreSQL, use SQLite instead."),
				2: userTurn("Final call: back to PostgreSQL after all."),
			},
			question: "Which datastore is the current decision? Reply with one word.",
			want:     []string{"postgresql", "postgres"},
			reject:   []string{"sqlite"},
		},
		{
			class:   "distractor-file",
			plantAt: 1,
			plant:   userTurn("Draft note: the fix might belong in config/legacy_parser.go."),
			later: map[int]func(*agent.Session){
				3: userTurn("Confirmed: the fix belongs in config/emitter.go; the legacy parser is not involved."),
			},
			question: "Which file does the fix belong in? Reply with just the filename, no path.",
			want:     []string{"emitter"},
			reject:   []string{"legacy"},
		},
	}
}

// score reports whether an answer keeps the planted fact. A rejected token
// anywhere in the answer counts as lost even when the wanted token also
// appears, so "yes, but it was not re-run" does not pass as "no".
func (p probe) score(answer string) bool {
	a := strings.ToLower(answer)
	for _, bad := range p.reject {
		if matchesToken(a, bad) {
			return false
		}
	}
	for _, good := range p.want {
		if matchesToken(a, good) {
			return true
		}
	}
	return false
}

// matchesToken compares whole words, never substrings: "I am not sure" must not
// count as the answer "no". Multi-word wants are phrases and match literally.
func matchesToken(lowered, want string) bool {
	if strings.Contains(want, " ") {
		return strings.Contains(lowered, want)
	}
	return slices.Contains(strings.FieldsFunc(lowered, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	}), want)
}

// settledAt is the first generation whose answer is the one want/reject scores.
// A probe revised across folds has a different correct answer while the chain is
// still running, so asking before it settles would score a right answer as lost.
func (p probe) settledAt() int {
	at := p.plantAt
	for gen := range p.later {
		at = max(at, gen)
	}
	return at
}

func (p probe) String() string { return fmt.Sprintf("%s@gen%d", p.class, p.settledAt()) }
