package evidence

import (
	"path/filepath"
	"strings"

	"reasonix/internal/shellparse"
)

// CommandMatches reports whether a cited verification command is proven by a
// command that actually ran. Models paraphrase commands when citing them
// (dropping a `cd` prefix, changing quote style, omitting flags), so byte
// equality rejects real verifications; instead both sides are split into
// shell segments and each cited segment must be covered by some ran segment.
func CommandMatches(cited, ran string) bool {
	citedSegs := commandSegments(cited)
	if len(citedSegs) == 0 {
		return false
	}
	ranSegs := commandSegments(ran)
	for _, c := range citedSegs {
		if !segmentCovered(c, ranSegs) {
			return false
		}
	}
	return true
}

func segmentCovered(cited string, ranSegs []string) bool {
	for _, r := range ranSegs {
		if segmentMatches(cited, r) {
			return true
		}
	}
	return false
}

// segmentMatches accepts normalized equality, or a token subset with the same
// head token (e.g. cited "ls x 2>&1" against ran "ls -la x 2>&1"). One-token
// citations only match exactly, so a bare "ls" can't claim an unrelated run.
func segmentMatches(cited, ran string) bool {
	ct, rt := segmentTokens(cited), segmentTokens(ran)
	if len(ct) == 0 || len(rt) == 0 {
		return false
	}
	if strings.Join(ct, " ") == strings.Join(rt, " ") {
		return true
	}
	if len(ct) < 2 || ct[0] != rt[0] {
		return false
	}
	have := make(map[string]bool, len(rt))
	for _, t := range rt {
		have[t] = true
	}
	for _, t := range ct {
		if !have[t] {
			return false
		}
	}
	return true
}

var segmentSeparators = []string{"&&", "||", ";", "|", "\n"}

func commandSegments(s string) []string {
	if segs, _, ok := shellparse.SplitTopLevel(s); ok {
		return segs
	}
	parts := []string{s}
	for _, sep := range segmentSeparators {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	var segs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		segs = append(segs, p)
	}
	return segs
}

func segmentTokens(s string) []string {
	if fields, malformed := shellparse.StaticFields(s); malformed == "" {
		return fields
	}
	fields := strings.Fields(s)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		f = strings.ReplaceAll(f, "'", "")
		if f != "" {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

func (l *Ledger) HasSuccessfulCommand(command string) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.ToolName == "bash" && CommandMatches(command, r.Command) {
			return true
		}
	}
	return false
}

// HasFailedCommand reports whether the cited command ran this turn but exited
// non-zero — so callers can distinguish "ran and failed" from "never ran".
func (l *Ledger) HasFailedCommand(command string) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success && r.ToolName == "bash" && CommandMatches(command, r.Command) {
			return true
		}
	}
	return false
}

// SuccessfulCommands returns up to limit successful bash commands from this
// turn, most recent first, for self-correction hints in rejection errors.
func (l *Ledger) SuccessfulCommands(limit int) []string {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := len(l.receipts) - 1; i >= 0 && len(out) < limit; i-- {
		r := l.receipts[i]
		if r.Success && r.ToolName == "bash" && r.Command != "" {
			out = append(out, r.Command)
		}
	}
	return out
}

// HasSuccessfulBashMentioningPaths reports whether every path appears in some
// successful bash command this turn — files created or edited through shell
// redirection (`seq … > file`) leave no reader/writer receipt, so the command
// text naming the path is the receipt.
func (l *Ledger) HasSuccessfulBashMentioningPaths(paths []string) bool {
	wanted := normalizePaths(paths)
	if l == nil || len(wanted) == 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range wanted {
		needle := strings.ToLower(filepath.ToSlash(p))
		found := false
		for _, r := range l.receipts {
			if !r.Success || r.ToolName != "bash" {
				continue
			}
			command := strings.ToLower(strings.ReplaceAll(r.Command, `\`, `/`))
			if strings.Contains(command, needle) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (l *Ledger) HasSuccessfulCommandAfter(command string, after int) bool {
	command = strings.TrimSpace(command)
	if l == nil || command == "" {
		return false
	}
	start := max(after+1, 0)

	l.mu.Lock()
	defer l.mu.Unlock()
	for i := start; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if r.Success && r.ToolName == "bash" && CommandMatches(command, r.Command) {
			return true
		}
	}
	return false
}
