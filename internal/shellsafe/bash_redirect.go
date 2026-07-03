package shellsafe

import (
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/shellparse"
)

// NormalizeBashSafeRedirectsForMatch returns a copy of subject with redirect
// syntax removed only when the redirect cannot write to a real file. It is used
// for shell safety matching, never for execution.
//
// Supported safe forms are fd duplication/close (`2>&1`, `>&2`, `2>&-`,
// `0<&1`) and output redirects to a null sink (`>/dev/null`, `>$null`,
// `>nul`, `2> /dev/null`, `>>/dev/null`, `&>/dev/null`, `&>>/dev/null`).
// The shell execution layer normalizes these null-sink spellings to the actual
// sink for the resolved shell. Other redirections are left unnormalized so the
// usual shell-syntax guard keeps prefix/read-only matching conservative.
func NormalizeBashSafeRedirectsForMatch(subject string) (string, bool) {
	file, err := shellparse.ParseBash(subject)
	if err != nil || shellparse.HasHereDoc(file) {
		return "", false
	}
	spans, ok := safeRedirectSpans(subject, file.Stmts)
	if !ok {
		return "", false
	}
	if len(spans) == 0 {
		return subject, true
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var out strings.Builder
	last := 0
	for _, span := range spans {
		if span.start < last || span.end > len(subject) {
			return "", false
		}
		out.WriteString(subject[last:span.start])
		last = span.end
	}
	out.WriteString(subject[last:])
	return strings.TrimSpace(out.String()), true
}

type redirectSpan struct {
	start int
	end   int
}

func safeRedirectSpans(source string, stmts []*syntax.Stmt) ([]redirectSpan, bool) {
	var spans []redirectSpan
	for _, stmt := range stmts {
		if !appendSafeRedirectSpans(source, stmt, &spans) {
			return nil, false
		}
	}
	return spans, true
}

func appendSafeRedirectSpans(source string, stmt *syntax.Stmt, spans *[]redirectSpan) bool {
	if stmt == nil {
		return true
	}
	for _, redir := range stmt.Redirs {
		span, ok := safeRedirectSpan(source, redir)
		if !ok {
			return false
		}
		*spans = append(*spans, span)
	}
	if binary, ok := stmt.Cmd.(*syntax.BinaryCmd); ok {
		return appendSafeRedirectSpans(source, binary.X, spans) &&
			appendSafeRedirectSpans(source, binary.Y, spans)
	}
	return true
}

func safeRedirectSpan(source string, redir *syntax.Redirect) (redirectSpan, bool) {
	if redir == nil {
		return redirectSpan{}, false
	}
	switch redir.Op {
	case syntax.DplOut, syntax.DplIn:
		if !isSafeFDDupWord(source, redir.Word) {
			return redirectSpan{}, false
		}
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.AppAll, syntax.RdrAllClob, syntax.AppAllClob:
		if !isNullRedirectWord(source, redir.Word) {
			return redirectSpan{}, false
		}
	default:
		return redirectSpan{}, false
	}
	start := int(redir.OpPos.Offset())
	if redir.N != nil && redir.N.Pos().IsValid() {
		start = int(redir.N.Pos().Offset())
	}
	end := int(redir.End().Offset())
	if start < 0 || end < start || end > len(source) {
		return redirectSpan{}, false
	}
	return redirectSpan{start: start, end: end}, true
}

func isSafeFDDupWord(source string, word *syntax.Word) bool {
	value := redirectWordSource(source, word)
	if value == "-" {
		return true
	}
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func isNullRedirectWord(source string, word *syntax.Word) bool {
	value := redirectWordSource(source, word)
	if value == "/dev/null" {
		return true
	}
	return strings.EqualFold(value, "$null") || strings.EqualFold(value, "nul")
}

func redirectWordSource(source string, word *syntax.Word) string {
	if word == nil || !word.Pos().IsValid() || !word.End().IsValid() {
		return ""
	}
	start := int(word.Pos().Offset())
	end := int(word.End().Offset())
	if start < 0 || end < start || end > len(source) {
		return ""
	}
	return strings.TrimSpace(source[start:end])
}
