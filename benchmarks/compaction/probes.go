package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
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
	class    string
	plantAt  int
	plant    func(*agent.Session)
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

func (p probe) String() string { return fmt.Sprintf("%s@gen%d", p.class, p.plantAt) }
