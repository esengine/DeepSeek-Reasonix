package shellparse

import (
	"errors"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ParseBash parses command using Bash syntax.
func ParseBash(command string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
}

// StaticCommandPolicy controls which static shell features may be modeled
// without invoking a shell.
type StaticCommandPolicy struct {
	AllowEnvAssignments bool
	AllowStderrToStdout bool
}

// StaticCommand is a shell command reduced to exec.Command inputs.
type StaticCommand struct {
	Argv        []string
	Env         []string
	MergeStderr bool
}

// StaticRejectReason names why a command cannot be reduced to StaticCommand.
type StaticRejectReason string

const (
	StaticRejectParse       StaticRejectReason = "parse error"
	StaticRejectHereDoc     StaticRejectReason = "here document"
	StaticRejectControl     StaticRejectReason = "shell control syntax"
	StaticRejectRedirection StaticRejectReason = "shell redirection"
	StaticRejectAssignment  StaticRejectReason = "shell assignment"
	StaticRejectExpansion   StaticRejectReason = "shell expansion"
)

// StaticRejectError carries a machine-readable rejection reason plus optional
// parser detail.
type StaticRejectError struct {
	Reason StaticRejectReason
	Detail string
}

func (e *StaticRejectError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail != "" {
		return e.Detail
	}
	return string(e.Reason)
}

func staticReject(reason StaticRejectReason, detail string) *StaticRejectError {
	return &StaticRejectError{Reason: reason, Detail: detail}
}

// StaticFields returns the fields of a single static Bash command. It rejects
// shell syntax that can alter command shape, such as control operators,
// redirects, assignments, backgrounding, and runtime expansions.
func StaticFields(command string) ([]string, string) {
	cmd, err := ParseStaticCommand(command, StaticCommandPolicy{})
	if err != nil {
		return nil, staticFieldsMessage(err)
	}
	return cmd.Argv, ""
}

// ParseStaticCommand parses a single static Bash command into argv and optional
// environment assignments. It never evaluates shell expansion or runs a shell.
func ParseStaticCommand(command string, policy StaticCommandPolicy) (StaticCommand, error) {
	var out StaticCommand
	if strings.TrimSpace(command) == "" {
		return out, nil
	}
	file, err := ParseBash(command)
	if err != nil {
		return out, staticReject(StaticRejectParse, err.Error())
	}
	if HasHereDoc(file) {
		return out, staticReject(StaticRejectHereDoc, "")
	}
	if len(file.Stmts) != 1 {
		return out, staticReject(StaticRejectControl, "")
	}
	stmt := file.Stmts[0]
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return out, staticReject(StaticRejectControl, "")
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return out, staticReject(StaticRejectControl, "")
	}
	if len(stmt.Redirs) > 0 {
		mergeStderr, err := staticRedirections(stmt.Redirs, policy)
		if err != nil {
			return out, err
		}
		out.MergeStderr = mergeStderr
	}
	if len(call.Assigns) > 0 {
		env, err := staticAssignments(call.Assigns, policy)
		if err != nil {
			return out, err
		}
		out.Env = env
	}

	out.Argv = make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		field, ok := StaticWord(arg)
		if !ok {
			return out, staticReject(StaticRejectExpansion, "")
		}
		out.Argv = append(out.Argv, field)
	}
	if len(out.Argv) == 0 && len(out.Env) > 0 {
		return StaticCommand{}, staticReject(StaticRejectAssignment, "shell assignment without command")
	}
	return out, nil
}

func staticFieldsMessage(err error) string {
	var reject *StaticRejectError
	if !errors.As(err, &reject) {
		return err.Error()
	}
	switch reject.Reason {
	case StaticRejectParse:
		return reject.Error()
	case StaticRejectHereDoc:
		return "here document"
	case StaticRejectExpansion:
		return "shell expansion"
	default:
		return "shell control syntax"
	}
}

func staticAssignments(assigns []*syntax.Assign, policy StaticCommandPolicy) ([]string, error) {
	if !policy.AllowEnvAssignments {
		return nil, staticReject(StaticRejectAssignment, "")
	}
	env := make([]string, 0, len(assigns))
	for _, assign := range assigns {
		if assign == nil || assign.Append || assign.Naked || assign.Name == nil || assign.Index != nil || assign.Array != nil {
			return nil, staticReject(StaticRejectAssignment, "")
		}
		value := ""
		if assign.Value != nil {
			var ok bool
			value, ok = StaticWord(assign.Value)
			if !ok {
				return nil, staticReject(StaticRejectExpansion, "")
			}
		}
		env = append(env, assign.Name.Value+"="+value)
	}
	return env, nil
}

func staticRedirections(redirs []*syntax.Redirect, policy StaticCommandPolicy) (bool, error) {
	mergeStderr := false
	for _, redir := range redirs {
		if !policy.AllowStderrToStdout || !isStderrToStdout(redir) || mergeStderr {
			return false, staticReject(StaticRejectRedirection, "")
		}
		mergeStderr = true
	}
	return mergeStderr, nil
}

func isStderrToStdout(redir *syntax.Redirect) bool {
	if redir == nil || redir.Op != syntax.DplOut || redir.N == nil || redir.N.Value != "2" {
		return false
	}
	word, ok := StaticWord(redir.Word)
	return ok && word == "1"
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
