package shellparse

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ParseBash parses command using Bash syntax.
func ParseBash(command string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
}

// StaticFields returns the fields of a single static Bash command. It rejects
// shell syntax that can alter command shape, such as control operators,
// redirects, assignments, backgrounding, and runtime expansions.
func StaticFields(command string) ([]string, string) {
	if strings.TrimSpace(command) == "" {
		return nil, ""
	}
	file, err := ParseBash(command)
	if err != nil {
		return nil, err.Error()
	}
	if HasHereDoc(file) {
		return nil, "here document"
	}
	if len(file.Stmts) != 1 {
		return nil, "shell control syntax"
	}
	stmt := file.Stmts[0]
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
		return nil, "shell control syntax"
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 {
		return nil, "shell control syntax"
	}

	fields := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		field, ok := StaticWord(arg)
		if !ok {
			return nil, "shell expansion"
		}
		fields = append(fields, field)
	}
	return fields, ""
}

// ContainsShellSyntax reports whether command is anything other than a single
// static Bash command. Parse failures are treated as syntax to keep callers
// conservative.
func ContainsShellSyntax(command string) bool {
	if strings.TrimSpace(command) == "" {
		return false
	}
	_, malformed := StaticFields(command)
	return malformed != ""
}

// SplitTopLevel returns simple command segments split at top-level shell
// control operators. It preserves each segment's original source text. ok is
// false when the command cannot be decomposed without losing safety.
func SplitTopLevel(command string) (segments []string, split bool, ok bool) {
	if strings.TrimSpace(command) == "" {
		return nil, false, true
	}
	file, err := ParseBash(command)
	if err != nil || HasHereDoc(file) {
		return nil, false, false
	}

	for _, stmt := range file.Stmts {
		if len(file.Stmts) > 1 {
			split = true
		}
		if !appendTopLevelSegments(command, stmt, &segments, &split) {
			return nil, false, false
		}
	}
	segments = compactSegments(segments)
	return segments, split, true
}

func appendTopLevelSegments(source string, stmt *syntax.Stmt, segments *[]string, split *bool) bool {
	if stmt == nil || stmt.Negated || stmt.Coprocess || stmt.Disown {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if stmt.Background || len(stmt.Redirs) > 0 {
			return false
		}
		*split = true
		return appendTopLevelSegments(source, cmd.X, segments, split) &&
			appendTopLevelSegments(source, cmd.Y, segments, split)
	case *syntax.CallExpr:
		segment := sourceForStmt(source, stmt)
		if segment != "" {
			*segments = append(*segments, segment)
		}
		if stmt.Background {
			*split = true
		}
		return true
	default:
		return false
	}
}

func sourceForStmt(source string, stmt *syntax.Stmt) string {
	start := int(stmt.Pos().Offset())
	end := int(stmt.End().Offset())
	if stmt.Semicolon.IsValid() {
		semi := int(stmt.Semicolon.Offset())
		if start <= semi && semi <= end {
			end = semi
		}
	}
	if start < 0 || end < start || end > len(source) {
		return ""
	}
	return strings.TrimSpace(source[start:end])
}

func compactSegments(in []string) []string {
	out := in[:0]
	for _, segment := range in {
		segment = strings.TrimSpace(segment)
		if segment == "" || strings.HasPrefix(segment, "#") {
			continue
		}
		out = append(out, segment)
	}
	return out
}

// HasHereDoc reports whether file contains a here-document. Here-doc bodies are
// arbitrary text, so callers that analyze shell syntax should usually fail
// closed when this returns true.
func HasHereDoc(file *syntax.File) bool {
	if file == nil {
		return false
	}
	has := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil || has {
			return false
		}
		if redir, ok := node.(*syntax.Redirect); ok && redir.Hdoc != nil {
			has = true
			return false
		}
		return true
	})
	return has
}

// StaticWord returns word's static value, accepting literal and quoted literal
// parts while rejecting runtime expansions.
func StaticWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range word.Parts {
		value, ok := staticWordPart(part, false)
		if !ok {
			return "", false
		}
		b.WriteString(value)
	}
	return b.String(), true
}

func staticWordPart(part syntax.WordPart, inDoubleQuotes bool) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit:
		return unescapeLit(p.Value, inDoubleQuotes), true
	case *syntax.SglQuoted:
		return p.Value, true
	case *syntax.DblQuoted:
		var b strings.Builder
		for _, nested := range p.Parts {
			value, ok := staticWordPart(nested, true)
			if !ok {
				return "", false
			}
			b.WriteString(value)
		}
		return b.String(), true
	default:
		return "", false
	}
}

func unescapeLit(s string, inDoubleQuotes bool) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		next := s[i+1]
		if next == '\n' {
			i++
			continue
		}
		if !inDoubleQuotes || next == '$' || next == '`' || next == '"' || next == '\\' {
			b.WriteByte(next)
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// IsAssignment reports whether word has Bash assignment syntax.
func IsAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return false
			}
			continue
		}
		if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// WordBase returns the basename of a shell command word.
func WordBase(word string) string {
	if i := strings.LastIndexByte(word, '/'); i >= 0 {
		return word[i+1:]
	}
	return word
}
