package agent

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
)

// Acceptance used to ask a digest one question: is it smaller? A digest that
// summarized nothing answers yes. So a fold is also measured on the two losses
// that hurt — a change forgotten, and a failure forgotten and so repeated.

// foldCoverage is what one digest kept of the facts a fold had to carry.
type foldCoverage struct {
	Mutations  []string // files the fold changed
	Failures   []string // commands the fold saw fail
	MissingMut []string
	MissingFai []string
}

// Required is how many facts the digest had to carry.
func (c foldCoverage) Required() int { return len(c.Mutations) + len(c.Failures) }

// Missing is how many it dropped.
func (c foldCoverage) Missing() int { return len(c.MissingMut) + len(c.MissingFai) }

// LostAChange reports a forgotten change — the severity line, and one the
// mechanism owns rather than a ratio someone picked: a forgotten change makes
// the agent wrong, a forgotten failure only makes it slow.
func (c foldCoverage) LostAChange() bool { return len(c.MissingMut) > 0 }

// LostEveryChange reports a digest that carried none of the fold's changes.
// That is not a quality judgment needing a threshold — it is a broken digest.
func (c foldCoverage) LostEveryChange() bool {
	return len(c.Mutations) > 0 && len(c.MissingMut) == len(c.Mutations)
}

// Reason names what went missing, for the receipt and the retry instruction.
func (c foldCoverage) Reason() string {
	var parts []string
	if len(c.MissingMut) > 0 {
		parts = append(parts, "changed files not mentioned: "+strings.Join(c.MissingMut, ", "))
	}
	if len(c.MissingFai) > 0 {
		parts = append(parts, "failures not mentioned: "+strings.Join(c.MissingFai, ", "))
	}
	return strings.Join(parts, "; ")
}

// foldFacts derives what a fold must carry forward, through the same pure
// classifier the ledger uses — the live ledger is per-task and never spans a
// fold. readOnly decides whether a call counts as a change.
func foldFacts(region []provider.Message, facts func(string) evidence.ToolFacts) foldCoverage {
	var cov foldCoverage
	seenMut, seenFail := map[string]bool{}, map[string]bool{}
	calls := map[string]provider.ToolCall{}
	for _, m := range region {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = tc
		}
		if m.Role != provider.RoleTool {
			continue
		}
		call, ok := calls[m.ToolCallID]
		if !ok {
			continue
		}
		failed := isErrorMessage(m)
		rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), !failed, facts(call.Name))
		if !failed && rec.Mutation && len(rec.Paths) > 0 {
			for _, p := range rec.Paths {
				if p = strings.TrimSpace(p); p != "" && !seenMut[p] {
					seenMut[p] = true
					cov.Mutations = append(cov.Mutations, p)
				}
			}
		}
		if failed {
			if cmd := commandSignature(rec.Command); cmd != "" && !seenFail[cmd] {
				seenFail[cmd] = true
				cov.Failures = append(cov.Failures, cmd)
			}
		}
	}
	slices.Sort(cov.Mutations)
	slices.Sort(cov.Failures)
	return cov
}

// measureFoldCoverage checks a digest against the facts its fold produced.
func measureFoldCoverage(region []provider.Message, facts func(string) evidence.ToolFacts, digest string) foldCoverage {
	cov := foldFacts(region, facts)
	haystack := strings.ToLower(digest)
	for _, p := range cov.Mutations {
		if !mentionsPath(haystack, p) {
			cov.MissingMut = append(cov.MissingMut, p)
		}
	}
	for _, c := range cov.Failures {
		if !strings.Contains(haystack, strings.ToLower(c)) {
			cov.MissingFai = append(cov.MissingFai, c)
		}
	}
	return cov
}

// mentionsPath accepts the full path or the bare file name: insisting on the
// full path would fail a digest that did its job.
func mentionsPath(haystack, p string) bool {
	lower := strings.ToLower(p)
	if strings.Contains(haystack, lower) {
		return true
	}
	base := path.Base(filepath.ToSlash(lower))
	return base != "" && base != "." && base != "/" && strings.Contains(haystack, base)
}

// commandSignature reduces a command to what a digest would repeat: the program
// and its first argument.
func commandSignature(command string) string {
	fields, malformed := shellparse.StaticFields(strings.TrimSpace(command))
	if malformed != "" || len(fields) == 0 {
		if trimmed := strings.TrimSpace(command); trimmed != "" {
			return firstLine(trimmed)
		}
		return ""
	}
	if len(fields) > 2 {
		fields = fields[:2]
	}
	return strings.Join(fields, " ")
}

// foldBackstopHeading opens the host's own record of what a digest dropped.
const foldBackstopHeading = "## Host-retained fold facts"

// maxBackstopFacts bounds the block. A fold with more changes than this has a
// digest problem the backstop cannot fix; the count says so rather than the
// list silently ending.
const maxBackstopFacts = 20

// foldCoverageBackstop states the facts the digest did not carry. The index
// skips them on the digest's promise (coverageDemands), so a broken promise
// takes them out of the model's world while the transcript still holds them.
// This is the host's line, not a second summary: no tool output, no inference,
// only the paths and command signatures the receipts already proved.
func foldCoverageBackstop(cov foldCoverage) string {
	if cov.Missing() == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(foldBackstopHeading + "\n")
	b.WriteString("The summary above does not cover these, and the fold index omits them. The full transcript still holds them.\n")
	writeBackstopList(&b, "changed", cov.MissingMut)
	writeBackstopList(&b, "failed", cov.MissingFai)
	return strings.TrimRight(b.String(), "\n")
}

func writeBackstopList(b *strings.Builder, label string, facts []string) {
	shown := facts
	if len(shown) > maxBackstopFacts {
		shown = shown[:maxBackstopFacts]
	}
	for _, f := range shown {
		b.WriteString("- " + label + ": " + f + "\n")
	}
	if dropped := len(facts) - len(shown); dropped > 0 {
		fmt.Fprintf(b, "- (%d more %s, not listed)\n", dropped, label)
	}
}

// coverageDemands reports whether a call is one the digest must carry, so the
// index can take everything else and nothing falls between the two.
func coverageDemands(rec evidence.Receipt, failed bool) bool {
	if failed {
		return commandSignature(rec.Command) != ""
	}
	return rec.Mutation && len(rec.Paths) > 0
}
