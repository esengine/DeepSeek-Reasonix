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

// ApprovalFeatures describes Bash syntax that affects whether a permission can
// be reused. CommandPrefix contains the leading static argv fields up to the
// first runtime expansion; it lets callers recognize a static eval/-c command
// even when its payload is dynamic.
type ApprovalFeatures struct {
	CommandPrefix      []string
	DynamicCommandName bool
	NestedExecution    bool
	Expansion          bool
	Assignment         bool
	Redirection        bool
	// StdinHereDoc reports that the command reads its standard input from a
	// here-document. For an interpreter that is the same thing as -c: the code
	// arrives with the call rather than from a file the host can inspect.
	StdinHereDoc bool
}

// AnalyzeApprovalFeatures inspects one simple Bash command without evaluating
// expansions. ok is false for compound or otherwise unsupported statements.
func AnalyzeApprovalFeatures(command string) (features ApprovalFeatures, ok bool) {
	file, err := ParseBash(command)
	if err != nil || len(file.Stmts) != 1 {
		return features, false
	}
	stmt := file.Stmts[0]
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return features, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return features, false
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			features.NestedExecution = true
		case *syntax.ParamExp, *syntax.ArithmExp, *syntax.ExtGlob:
			features.Expansion = true
		case *syntax.Assign:
			features.Assignment = true
		case *syntax.Redirect:
			features.Redirection = true
		}
		return true
	})
	for _, redir := range stmt.Redirs {
		if redir == nil || redir.Hdoc == nil {
			continue
		}
		if redir.N == nil || redir.N.Value == "" || redir.N.Value == "0" {
			features.StdinHereDoc = true
		}
	}
	for _, arg := range call.Args {
		if wordHasUnescapedBrace(arg) {
			syntax.SplitBraces(arg)
		}
	}
	for i, arg := range call.Args {
		field, static := StaticWord(arg)
		if !static {
			features.Expansion = true
			if i == 0 {
				features.DynamicCommandName = true
			}
			break
		}
		features.CommandPrefix = append(features.CommandPrefix, field)
	}
	return features, true
}

// ContainsUnquotedGlob reports whether command contains an unquoted shell glob
// token. StaticFields deliberately returns argv without expanding globs, so
// permission callers use this additional check before reusing broad rules.
func ContainsUnquotedGlob(command string) bool {
	file, err := ParseBash(command)
	if err != nil {
		return true
	}
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		word, ok := node.(*syntax.Word)
		if !ok {
			return !found
		}
		for _, part := range word.Parts {
			lit, ok := part.(*syntax.Lit)
			if ok && hasUnescapedGlobMeta(lit.Value) {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func hasUnescapedGlobMeta(value string) bool {
	return hasUnescapedMeta(value, "*?[")
}

func wordHasUnescapedBrace(word *syntax.Word) bool {
	if word == nil {
		return false
	}
	for _, part := range word.Parts {
		if lit, ok := part.(*syntax.Lit); ok && hasUnescapedMeta(lit.Value, "{") {
			return true
		}
	}
	return false
}

func hasUnescapedMeta(value, meta string) bool {
	escaped := false
	for i := range len(value) {
		if escaped {
			escaped = false
			continue
		}
		if value[i] == '\\' {
			escaped = true
			continue
		}
		if strings.ContainsRune(meta, rune(value[i])) {
			return true
		}
	}
	return false
}

// CanMaskEarlierFailure reports whether a later part of command can hide the
// failure of an earlier part, so the shell's final exit status is not evidence
// that every step succeeded.
//
// Only `&&` chains are exempt: bash short-circuits them and reports the first
// failing command's status, so `build && test` already surfaces a failed build.
// Everything else can mask — `;` and newlines run the next command regardless,
// `||` runs it precisely when the previous one failed, `|` reports only the
// last stage, and `&` detaches the status entirely.
//
// ok is false when the command cannot be analyzed statically (parse failure,
// here-documents, unsupported control syntax); callers must not read canMask
// as proven-safe in that case.
func CanMaskEarlierFailure(command string) (canMask bool, ok bool) {
	if strings.TrimSpace(command) == "" {
		return false, true
	}
	file, err := ParseBash(command)
	if err != nil || HasHereDoc(file) {
		return false, false
	}
	// Two or more top-level statements are `;`/newline separated.
	if len(file.Stmts) > 1 {
		return true, true
	}
	for _, stmt := range file.Stmts {
		masks, stmtOK := stmtCanMaskEarlierFailure(stmt)
		if !stmtOK {
			return false, false
		}
		if masks {
			return true, true
		}
	}
	return false, true
}

func stmtCanMaskEarlierFailure(stmt *syntax.Stmt) (bool, bool) {
	if stmt == nil || stmt.Negated || stmt.Coprocess || stmt.Disown {
		return false, false
	}
	if stmt.Background {
		return true, true
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if cmd.Op != syntax.AndStmt {
			// `||`, `|`, and `|&` all let a later stage decide the status.
			return true, true
		}
		xMasks, xOK := stmtCanMaskEarlierFailure(cmd.X)
		if !xOK {
			return false, false
		}
		yMasks, yOK := stmtCanMaskEarlierFailure(cmd.Y)
		if !yOK {
			return false, false
		}
		return xMasks || yMasks, true
	case *syntax.CallExpr:
		return false, true
	default:
		return false, false
	}
}

// MasksOnlyInsideFinalPipeline reports whether the last pipeline is the only
// thing that could hide an earlier failure: every stage outside it short-circuits
// into the exit status. A caller that recovers per-stage statuses for that one
// pipeline can then read the whole command, which `;`, `||`, and background
// stages never allow.
func MasksOnlyInsideFinalPipeline(command string) bool {
	if strings.TrimSpace(command) == "" {
		return false
	}
	file, err := ParseBash(command)
	// More than one top-level statement means `;` or a newline, which drops the
	// earlier statement's status whatever the last pipeline reports.
	if err != nil || HasHereDoc(file) || len(file.Stmts) != 1 {
		return false
	}
	return stmtMasksOnlyInFinalPipeline(file.Stmts[0])
}

func stmtMasksOnlyInFinalPipeline(stmt *syntax.Stmt) bool {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.AndStmt:
			// The left side's failure short-circuits and becomes the status, so
			// it only stays readable while nothing inside it masks either.
			masks, ok := stmtCanMaskEarlierFailure(cmd.X)
			if !ok || masks {
				return false
			}
			return stmtMasksOnlyInFinalPipeline(cmd.Y)
		case syntax.Pipe, syntax.PipeAll:
			return true
		}
		return false
	case *syntax.CallExpr:
		return true
	default:
		return false
	}
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
	for i := range len(name) {
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

// CompoundLeafCommands returns the static argv of every command a compound
// statement would invoke, or ok=false when any part of it cannot be read
// statically. A true result is the complete list of what runs; classifying
// those argv is the caller's job.
func CompoundLeafCommands(command string) (leaves [][]string, ok bool) {
	file, err := ParseBash(command)
	if err != nil || len(file.Stmts) == 0 {
		return nil, false
	}
	// `;`-separated statements are several commands too. Only the chaining
	// operators used to set this, so a plain `a; b` fell between the
	// single-command classifier and this one and counted as a write.
	readable, compound := true, len(file.Stmts) > 1
	syntax.Walk(file, func(node syntax.Node) bool {
		if !readable || node == nil {
			return false
		}
		switch n := node.(type) {
		case *syntax.ForClause, *syntax.IfClause, *syntax.WhileClause,
			*syntax.CaseClause, *syntax.Subshell, *syntax.Block:
			// Only a statement that actually loops or branches goes down this
			// path. A simple command keeps its own stricter classification,
			// where a dynamic argument is already reason enough to stop.
			compound = true
		case *syntax.BinaryCmd:
			// A pipeline or an && / || chain is several commands, so the
			// single-command classifier cannot read it either. Without this it
			// fell between the two and every `… | head` counted as a write.
			compound = true
		case *syntax.CmdSubst, *syntax.ProcSubst:
			// Whatever these run never appears in the argv below.
			readable = false
			return false
		case *syntax.Redirect:
			// A redirect can create or truncate a file, and a here-document
			// feeds a program source the argv does not show.
			readable = false
			return false
		case *syntax.Stmt:
			if n.Negated || n.Background || n.Coprocess || n.Disown {
				readable = false
				return false
			}
		case *syntax.CallExpr:
			if len(n.Assigns) > 0 {
				readable = false
				return false
			}
			if len(n.Args) == 0 {
				return true // a bare assignment-less call has nothing to run
			}
			argv := make([]string, 0, len(n.Args))
			for i, arg := range n.Args {
				field, static := StaticWord(arg)
				if !static {
					// A non-static argument is fine as data — the program still
					// decides what it does — but never as the program itself.
					if i == 0 {
						readable = false
						return false
					}
					field = dynamicArgPlaceholder
				}
				argv = append(argv, field)
			}
			leaves = append(leaves, argv)
		}
		return true
	})
	if !readable || !compound || len(leaves) == 0 {
		return nil, false
	}
	return leaves, true
}

// dynamicArgPlaceholder stands in for an argument whose value is only known at
// run time. It never occupies argv[0], so a classifier still sees the real
// program name.
const dynamicArgPlaceholder = "__reasonix_dynamic_arg__"

// StdinHereDocPrograms returns the program name of every command in the
// statement whose standard input comes from a here-document, at any nesting.
// The caller decides which programs treat that input as source code.
func StdinHereDocPrograms(command string) []string {
	var programs []string
	for _, argv := range StdinHereDocArgv(command) {
		programs = append(programs, argv[0])
	}
	return programs
}

// StdinHereDocArgv is StdinHereDocPrograms with the arguments kept, so a caller
// can tell an interpreter handed a program from one handed a script to run and
// data to feed it. The body is never returned; only what was pointed at it.
func StdinHereDocArgv(command string) [][]string {
	file, err := ParseBash(command)
	if err != nil {
		return nil
	}
	var argvs [][]string
	syntax.Walk(file, func(node syntax.Node) bool {
		stmt, ok := node.(*syntax.Stmt)
		if !ok || stmt == nil {
			return true
		}
		fedFromHereDoc := false
		for _, redir := range stmt.Redirs {
			if redir == nil || redir.Hdoc == nil {
				continue
			}
			if redir.N == nil || redir.N.Value == "" || redir.N.Value == "0" {
				fedFromHereDoc = true
			}
		}
		if !fedFromHereDoc {
			return true
		}
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		program, static := StaticWord(call.Args[0])
		if !static {
			return true
		}
		argv := []string{program}
		for _, arg := range call.Args[1:] {
			word, static := StaticWord(arg)
			if !static {
				word = dynamicArgPlaceholder
			}
			argv = append(argv, word)
		}
		argvs = append(argvs, argv)
		return true
	})
	return argvs
}
