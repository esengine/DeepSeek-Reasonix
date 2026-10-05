package shellparse

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// HasHereDoc reports whether file contains a here-document. Here-doc bodies are
// arbitrary text, so callers that analyze shell syntax should usually fail
// closed when this returns true.
func HasHereDoc(file *syntax.File) bool {
	if file == nil {
		return false
	}
	return hereDocWithin(file)
}

// hereDocWithin reports whether any node under this one carries a here-doc, so
// a caller can scope the question to the statement it is actually reading
// rather than to the whole file.
func hereDocWithin(node syntax.Node) bool {
	if node == nil {
		return false
	}
	has := false
	syntax.Walk(node, func(n syntax.Node) bool {
		if n == nil || has {
			return false
		}
		if redir, ok := n.(*syntax.Redirect); ok && redir.Hdoc != nil {
			has = true
			return false
		}
		return true
	})
	return has
}

// SplitOutsideHereDoc decomposes command as SplitTopLevel does, except a leaf
// carrying a here-document is dropped rather than failing the whole command.
// The body is never decomposed either way. Callers deciding what may run keep
// using SplitTopLevel, which fails closed; this answers only what a command
// that already ran contained.
func SplitOutsideHereDoc(command string) (segments []string, ok bool) {
	if strings.TrimSpace(command) == "" {
		return nil, true
	}
	file, err := ParseBash(command)
	if err != nil {
		return nil, false
	}
	for _, stmt := range file.Stmts {
		if !appendSegmentsOutsideHereDoc(command, stmt, &segments) {
			return nil, false
		}
	}
	return compactSegments(segments), true
}

func appendSegmentsOutsideHereDoc(source string, stmt *syntax.Stmt, segments *[]string) bool {
	if stmt == nil || stmt.Negated || stmt.Coprocess || stmt.Disown {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if stmt.Background || len(stmt.Redirs) > 0 {
			return false
		}
		return appendSegmentsOutsideHereDoc(source, cmd.X, segments) &&
			appendSegmentsOutsideHereDoc(source, cmd.Y, segments)
	case *syntax.CallExpr:
		if hereDocWithin(stmt) {
			return true
		}
		if segment := sourceForStmt(source, stmt); segment != "" {
			*segments = append(*segments, segment)
		}
		return true
	default:
		return false
	}
}
