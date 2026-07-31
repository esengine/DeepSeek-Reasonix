// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode"

	"mvdan.cc/sh/v3/fileutil"
)

// PrinterOption is a function which can be passed to NewPrinter
// to alter its behavior. To apply option to existing Printer
// call it directly, for example KeepPadding(true)(printer).
type PrinterOption func(*Printer)

// Indent sets the number of spaces used for indentation. If set to 0,
// tabs will be used instead.
func Indent(spaces uint) PrinterOption {
	return func(p *Printer) { p.indentSpaces = spaces }
}

// BinaryNextLine will make binary operators appear on the next line
// when a binary command, such as a pipe, spans multiple lines. A
// backslash will be used.
func BinaryNextLine(enabled bool) PrinterOption {
	return func(p *Printer) { p.binNextLine = enabled }
}

// SwitchCaseIndent will make switch cases be indented. As such, switch
// case bodies will be two levels deeper than the switch itself.
func SwitchCaseIndent(enabled bool) PrinterOption {
	return func(p *Printer) { p.swtCaseIndent = enabled }
}

// TODO(v4): consider turning this into a "space all operators" option, to also
// allow foo=( bar baz ), (( x + y )), and so on.

// SpaceRedirects will put a space after most redirection operators. The
// exceptions are '>&', '<&', '>(', and '<('.
func SpaceRedirects(enabled bool) PrinterOption {
	return func(p *Printer) { p.spaceRedirects = enabled }
}

// KeepPadding will keep most nodes and tokens in the same column that
// they were in the original source. This allows the user to decide how
// to align and pad their code with spaces.
//
// Note that this feature is best-effort and will only keep the
// alignment stable, so it may need some human help the first time it is
// run.
//
// Deprecated: this formatting option is flawed and buggy, and often does
// not result in what the user wants when the code gets complex enough.
// The next major version, v4, will remove this feature entirely.
// See: https://github.com/mvdan/sh/issues/658
func KeepPadding(enabled bool) PrinterOption {
	return func(p *Printer) {
		if enabled && !p.keepPadding {
			// Enable the flag, and set up the writer wrapper.
			p.keepPadding = true
			p.cols.Writer = p.w.(*bufio.Writer)
			p.w = &p.cols

		} else if !enabled && p.keepPadding {
			// Ensure we reset the state to that of NewPrinter.
			p.keepPadding = false
			p.w = p.cols.Writer
			p.cols = colCounter{}
		}
	}
}

// Minify will print programs in a way to save the most bytes possible.
// For example, indentation and comments are skipped, and extra
// whitespace is avoided when possible.
func Minify(enabled bool) PrinterOption {
	return func(p *Printer) { p.minify = enabled }
}

// SingleLine will attempt to print programs in one line. For example, lists of
// commands or nested blocks do not use newlines in this mode. Note that some
// newlines must still appear, such as those following comments or around
// here-documents.
//
// Print's trailing newline when given a [*File] is not affected by this option.
func SingleLine(enabled bool) PrinterOption {
	return func(p *Printer) { p.singleLine = enabled }
}

// FunctionNextLine will place a function's opening braces on the next line.
func FunctionNextLine(enabled bool) PrinterOption {
	return func(p *Printer) { p.funcNextLine = enabled }
}

// NewPrinter allocates a new Printer and applies any number of options.
func NewPrinter(opts ...PrinterOption) *Printer {
	p := &Printer{
		w:         bufio.NewWriter(nil),
		tabWriter: new(tabwriter.Writer),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Print "pretty-prints" the given syntax tree node to the given writer. Writes
// to w are buffered.
//
// The node types supported at the moment are [*File], [*Stmt], [*Word], [*Assign], any
// [Command] node, and any WordPart node. A trailing newline will only be printed
// when a [*File] is used.
func (p *Printer) Print(w io.Writer, node Node) error {
	p.reset()

	if p.minify && p.singleLine {
		return fmt.Errorf("Minify and SingleLine together are not supported yet; please file an issue describing your use case: https://github.com/mvdan/sh/issues")
	}

	// TODO: consider adding a raw mode to skip the tab writer, much like in
	// go/printer.
	twmode := tabwriter.DiscardEmptyColumns | tabwriter.StripEscape
	tabwidth := 8
	if p.indentSpaces == 0 {
		// indenting with tabs
		twmode |= tabwriter.TabIndent
	} else {
		// indenting with spaces
		tabwidth = int(p.indentSpaces)
	}
	p.tabWriter.Init(w, 0, tabwidth, 1, ' ', twmode)
	w = p.tabWriter

	p.w.Reset(w)
	switch node := node.(type) {
	case *File:
		p.stmtList(node.Stmts, node.Last)
		p.newline(Pos{})
	case *Stmt:
		p.stmtList([]*Stmt{node}, nil)
	case Command:
		p.command(node, nil)
	case *Word:
		p.line = node.Pos().Line()
		p.word(node)
	case WordPart:
		p.line = node.Pos().Line()
		p.wordPart(node, nil)
	case *Assign:
		p.line = node.Pos().Line()
		p.assigns([]*Assign{node})
	default:
		return fmt.Errorf("unsupported node type: %T", node)
	}
	p.flushHeredocs()
	p.flushComments()

	// flush the writers
	if err := p.w.Flush(); err != nil {
		return err
	}
	if tw, _ := w.(*tabwriter.Writer); tw != nil {
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

type bufWriter interface {
	Write([]byte) (int, error)
	WriteString(string) (int, error)
	WriteByte(byte) error
	Reset(io.Writer)
	Flush() error
}

type colCounter struct {
	*bufio.Writer
	column    int
	lineStart bool
}

func (c *colCounter) addByte(b byte) {
	switch b {
	case '\n':
		c.column = 0
		c.lineStart = true
	case '\t', ' ', tabwriter.Escape:
	default:
		c.lineStart = false
	}
	c.column++
}

func (c *colCounter) WriteByte(b byte) error {
	c.addByte(b)
	return c.Writer.WriteByte(b)
}

func (c *colCounter) WriteString(s string) (int, error) {
	for _, b := range []byte(s) {
		c.addByte(b)
	}
	return c.Writer.WriteString(s)
}

func (c *colCounter) Reset(w io.Writer) {
	c.column = 1
	c.lineStart = true
	c.Writer.Reset(w)
}

// Printer holds the internal state of the printing mechanism of a
// program.
type Printer struct {
	w         bufWriter
	tabWriter *tabwriter.Writer
	cols      colCounter // used for [KeepPadding]

	indentSpaces   uint
	binNextLine    bool
	swtCaseIndent  bool
	spaceRedirects bool
	keepPadding    bool
	minify         bool
	singleLine     bool
	funcNextLine   bool

	wantSpace wantSpaceState // whether space is required or has been written

	wantNewline bool // newline is wanted for pretty-printing; ignored by singleLine; ignored by singleLine
	mustNewline bool // newline is required to keep shell syntax valid
	wroteSemi   bool // wrote ';' for the current statement

	// pendingComments are any comments in the current line or statement
	// that we have yet to print. This is useful because that way, we can
	// ensure that all comments are written immediately before a newline.
	// Otherwise, in some edge cases we might wrongly place words after a
	// comment in the same line, breaking programs.
	pendingComments []Comment

	// firstLine means we are still writing the first line
	firstLine bool
	// line is the current line number
	line uint

	// lastLevel is the last level of indentation that was used.
	lastLevel uint
	// level is the current level of indentation.
	level uint
	// levelIncs records which indentation level increments actually
	// took place, to revert them once their section ends.
	levelIncs []bool

	nestedBinary bool

	// pendingHdocs is the list of pending heredocs to write.
	pendingHdocs []*Redirect

	// used when printing <<- heredocs with tab indentation
	tabsPrinter *Printer
}

func (p *Printer) reset() {
	p.wantSpace = spaceWritten
	p.wantNewline, p.mustNewline = false, false
	p.pendingComments = p.pendingComments[:0]

	// minification uses its own newline logic
	p.firstLine = !p.minify
	p.line = 0

	p.lastLevel, p.level = 0, 0
	p.levelIncs = p.levelIncs[:0]
	p.nestedBinary = false
	p.pendingHdocs = p.pendingHdocs[:0]
}

func (p *Printer) spaces(n uint) {
	for range n {
		p.w.WriteByte(' ')
	}
}

func (p *Printer) space() {
	p.w.WriteByte(' ')
	p.wantSpace = spaceWritten
}

func (p *Printer) spacePad(pos Pos) {
	if p.cols.lineStart && p.indentSpaces == 0 {
		// Never add padding at the start of a line unless we are indenting
		// with spaces, since this may result in mixing of spaces and tabs.
		return
	}
	if p.wantSpace == spaceRequired {
		p.w.WriteByte(' ')
		p.wantSpace = spaceWritten
	}
	for p.cols.column > 0 && p.cols.column < int(pos.Col()) {
		p.w.WriteByte(' ')
	}
}

// wantsNewline reports whether we want to print at least one newline before
// printing a node at a given position. A zero position can be given to simply
// tell if we want a newline following what's just been printed.
func (p *Printer) wantsNewline(pos Pos, escapingNewline bool) bool {
	if p.mustNewline {
		// We must have a newline here.
		return true
	}
	if p.singleLine && len(p.pendingComments) == 0 {
		// The newline is optional, and singleLine skips it.
		// Don't skip if there are any pending comments,
		// as that might move them further down to the wrong place.
		return false
	}
	if escapingNewline && p.minify {
		return false
	}
	// The newline is optional, and we want it via either wantNewline or via
	// the position's line.
	return p.wantNewline || pos.Line() > p.line
}

func (p *Printer) bslashNewl() {
	if p.wantSpace == spaceRequired {
		p.space()
	}
	p.w.WriteString("\\\n")
	p.line++
	p.indent()
}

func (p *Printer) spacedString(s string, pos Pos) {
	p.spacePad(pos)
	p.w.WriteString(s)
	p.wantSpace = spaceRequired
}

func (p *Printer) spacedToken(s string, pos Pos) {
	if p.minify {
		p.w.WriteString(s)
		p.wantSpace = spaceNotRequired
		return
	}
	p.spacePad(pos)
	p.w.WriteString(s)
	p.wantSpace = spaceRequired
}

func (p *Printer) semiOrNewl(s string, pos Pos) {
	if p.wantsNewline(Pos{}, false) {
		p.newline(pos)
		p.indent()
	} else {
		if !p.wroteSemi {
			p.w.WriteByte(';')
		}
		if !p.minify {
			p.space()
		}
		p.advanceLine(pos.Line())
	}
	p.w.WriteString(s)
	p.wantSpace = spaceRequired
}

func (p *Printer) writeLit(s string) {
	// If p.tabWriter is nil, this is the nested printer being used to print
	// <<- heredoc bodies, so the parent printer will add the escape bytes
	// later.
	if p.tabWriter != nil && strings.Contains(s, "\t") {
		p.w.WriteByte(tabwriter.Escape)
		defer p.w.WriteByte(tabwriter.Escape)
	}
	p.w.WriteString(s)
}

func (p *Printer) incLevel() {
	inc := false
	if p.level <= p.lastLevel || len(p.levelIncs) == 0 {
		p.level++
		inc = true
	} else if last := &p.levelIncs[len(p.levelIncs)-1]; *last {
		*last = false
		inc = true
	}
	p.levelIncs = append(p.levelIncs, inc)
}

func (p *Printer) decLevel() {
	if p.levelIncs[len(p.levelIncs)-1] {
		p.level--
	}
	p.levelIncs = p.levelIncs[:len(p.levelIncs)-1]
}

func (p *Printer) indent() {
	if p.minify {
		return
	}
	p.lastLevel = p.level
	switch {
	case p.level == 0:
	case p.indentSpaces == 0:
		p.w.WriteByte(tabwriter.Escape)
		for i := uint(0); i < p.level; i++ {
			p.w.WriteByte('\t')
		}
		p.w.WriteByte(tabwriter.Escape)
	default:
		p.spaces(p.indentSpaces * p.level)
	}
}

// TODO(mvdan): add an indent call at the end of newline?

// newline prints one newline and advances p.line to pos.Line().
func (p *Printer) newline(pos Pos) {
	p.flushHeredocs()
	p.flushComments()
	p.w.WriteByte('\n')
	p.wantSpace = spaceWritten
	p.wantNewline, p.mustNewline = false, false
	p.advanceLine(pos.Line())
}

func (p *Printer) advanceLine(line uint) {
	if p.line < line {
		p.line = line
	}
}

func (p *Printer) flushHeredocs() {
	if len(p.pendingHdocs) == 0 {
		return
	}
	hdocs := p.pendingHdocs
	p.pendingHdocs = p.pendingHdocs[:0]
	coms := p.pendingComments
	p.pendingComments = nil
	if len(coms) > 0 {
		c := coms[0]
		if c.Pos().Line() == p.line {
			p.pendingComments = append(p.pendingComments, c)
			p.flushComments()
			coms = coms[1:]
		}
	}

	// Reuse the last indentation level, as
	// indentation levels are usually changed before
	// newlines are printed along with their
	// subsequent indentation characters.
	newLevel := p.level
	p.level = p.lastLevel

	for _, r := range hdocs {
		p.line++
		p.w.WriteByte('\n')
		p.wantSpace = spaceWritten
		p.wantNewline, p.wantNewline = false, false
		if r.Op == DashHdoc && p.indentSpaces == 0 && !p.minify {
			if r.Hdoc != nil {
				extra := extraIndenter{
					bufWriter:   p.w,
					baseIndent:  int(p.level + 1),
					firstIndent: -1,
				}
				p.tabsPrinter = &Printer{
					w: &extra,

					// The options need to persist.
					indentSpaces:   p.indentSpaces,
					binNextLine:    p.binNextLine,
					swtCaseIndent:  p.swtCaseIndent,
					spaceRedirects: p.spaceRedirects,
					keepPadding:    p.keepPadding,
					minify:         p.minify,
					funcNextLine:   p.funcNextLine,

					line: r.Hdoc.Pos().Line(),
				}
				p.tabsPrinter.wordParts(r.Hdoc.Parts, true)
			}
			p.indent()
		} else if r.Hdoc != nil {
			p.wordParts(r.Hdoc.Parts, true)
		}
		p.unquotedWord(r.Word)
		if r.Hdoc != nil {
			// Overwrite p.line, since printing r.Word again can set
			// p.line to the beginning of the heredoc again.
			p.advanceLine(r.Hdoc.End().Line())
		}
		p.wantSpace = spaceNotRequired
	}
	p.level = newLevel
	p.pendingComments = coms
	p.mustNewline = true
}

// newline prints between zero and two newlines.
// If any newlines are printed, it advances p.line to pos.Line().
func (p *Printer) newlines(pos Pos) {
	if p.firstLine && len(p.pendingComments) == 0 {
		p.firstLine = false
		return // no empty lines at the top
	}
	if !p.wantsNewline(pos, false) {
		return
	}
	p.flushHeredocs()
	p.flushComments()
	p.w.WriteByte('\n')
	p.wantSpace = spaceWritten
	p.wantNewline, p.mustNewline = false, false

	l := pos.Line()
	if l > p.line+1 && !p.minify {
		p.w.WriteByte('\n') // preserve single empty lines
	}
	p.advanceLine(l)
	p.indent()
}

func (p *Printer) rightParen(pos Pos) {
	if len(p.pendingHdocs) > 0 || !p.minify {
		p.newlines(pos)
	}
	p.w.WriteByte(')')
	p.wantSpace = spaceRequired
}

func (p *Printer) semiRsrv(s string, pos Pos) {
	if p.wantsNewline(pos, false) {
		p.newlines(pos)
	} else {
		if !p.wroteSemi {
			p.w.WriteByte(';')
		}
		if !p.minify {
			p.spacePad(pos)
		}
	}
	p.w.WriteString(s)
	p.wantSpace = spaceRequired
}

func (p *Printer) flushComments() {
	for i, c := range p.pendingComments {
		if i == 0 {
			// Flush any pending heredocs first. Otherwise, the
			// comments would become part of a heredoc body.
			p.flushHeredocs()
		}
		p.firstLine = false
		// We can't call any of the newline methods, as they call this
		// function and we'd recurse forever.
		cline := c.Hash.Line()
		switch {
		case p.mustNewline, i > 0, cline > p.line && p.line > 0:
			p.w.WriteByte('\n')
			if cline > p.line+1 {
				p.w.WriteByte('\n')
			}
			p.indent()
			p.wantSpace = spaceWritten
			p.spacePad(c.Pos())
		case p.wantSpace == spaceRequired:
			if p.keepPadding {
				p.spacePad(c.Pos())
			} else {
				p.w.WriteByte('\t')
			}
		case p.wantSpace != spaceWritten:
			p.space()
		}
		// don't go back one line, which may happen in some edge cases
		p.advanceLine(cline)
		p.w.WriteByte('#')
		p.writeLit(strings.TrimRightFunc(c.Text, unicode.IsSpace))
		p.wantNewline = true
		p.mustNewline = true
	}
	p.pendingComments = nil
}

func (p *Printer) comments(comments ...Comment) {
	if p.minify {
		for _, c := range comments {
			if fileutil.Shebang([]byte("#"+c.Text)) != "" && c.Hash.Col() == 1 && c.Hash.Line() == 1 {
				p.w.WriteString(strings.TrimRightFunc("#"+c.Text, unicode.IsSpace))
				p.w.WriteString("\n")
				p.line++
			}
		}
		return
	}
	p.pendingComments = append(p.pendingComments, comments...)
}

func (p *Printer) wordParts(wps []WordPart, quoted bool) {
	// We disallow unquoted escaped newlines between word parts below.
	// However, we want to allow a leading escaped newline for cases such as:
	//
	//   foo <<< \
	//     "bar baz"
	if !quoted && !p.singleLine && wps[0].Pos().Line() > p.line {
		p.bslashNewl()
	}
	for i, wp := range wps {
		var next WordPart
		if i+1 < len(wps) {
			next = wps[i+1]
		}
		// Keep escaped newlines separating word parts when quoted.
		// Note that those escaped newlines don't cause indentaiton.
		// When not quoted, we strip them out consistently,
		// because attempting to keep them would prevent indentation.
		// Can't use p.wantsNewline here, since this is only about
		// escaped newlines.
		for quoted && !p.singleLine && wp.Pos().Line() > p.line {
			p.w.WriteString("\\\n")
			p.line++
		}
		p.wordPart(wp, next)
		p.advanceLine(wp.End().Line())
	}
}

func (p *Printer) wordPart(wp, next WordPart) {
	switch wp := wp.(type) {
	case *Lit:
		p.writeLit(wp.Value)
	case *SglQuoted:
		if wp.Dollar {
			p.w.WriteByte('$')
		}
		p.w.WriteByte('\'')
		p.writeLit(wp.Value)
		p.w.WriteByte('\'')
		p.advanceLine(wp.End().Line())
	case *DblQuoted:
		p.dblQuoted(wp)
	case *CmdSubst:
		p.advanceLine(wp.Pos().Line())
		p.cmdSubst(wp)
	case *ParamExp:
		litCont := ";"
		if nextLit, ok := next.(*Lit); ok && nextLit.Value != "" {
			litCont = nextLit.Value[:1]
		}
		if p.minify && !wp.Short && wp.simple() {
			name := wp.Param.Value
			switch {
			case len(name) > 1 && !ValidName(name): // ${10}
			case ValidName(name + litCont): // ${var}cont
			default:
				x2 := *wp
				x2.Short = true
				p.paramExp(&x2)
				return
			}
		}
		p.paramExp(wp)
	case *ArithmExp:
		p.w.WriteString("$((")
		if wp.Unsigned {
			p.w.WriteString("# ")
		}
		p.arithmExpr(wp.X, false, false)
		p.w.WriteString("))")
	case *ExtGlob:
		p.w.WriteString(wp.Op.String())
		p.writeLit(wp.Pattern.Value)
		p.w.WriteByte(')')
	case *ProcSubst:
		// avoid conflict with << and others
		if p.wantSpace == spaceRequired {
			p.space()
		}
		p.w.WriteString(wp.Op.String())
		p.nestedStmts(wp.Stmts, wp.Last, wp.Rparen)
		p.rightParen(wp.Rparen)
	}
}

func (p *Printer) dblQuoted(dq *DblQuoted) {
	if dq.Dollar {
		p.w.WriteByte('$')
	}
	p.w.WriteByte('"')
	if len(dq.Parts) > 0 {
		p.wordParts(dq.Parts, true)
	}
	// Add any trailing escaped newlines.
	for p.line < dq.Right.Line() {
		p.w.WriteString("\\\n")
		p.line++
	}
	p.w.WriteByte('"')
}

func (p *Printer) wroteIndex(index ArithmExpr) bool {
	if index == nil {
		return false
	}
	p.w.WriteByte('[')
	// Note that e.g. foo[1,3]=$bar in Zsh does not allow any spaces around the comma,
	// as that breaks the assignment word.
	binary, ok := index.(*BinaryArithm)
	compact := ok && binary.Op == Comma
	p.arithmExpr(index, compact, false)
	p.w.WriteByte(']')
	return true
}

func (p *Printer) paramExp(pe *ParamExp) {
	if pe.nakedIndex() { // arr[x]
		p.writeLit(pe.Param.Value)
		p.wroteIndex(pe.Index)
		return
	}
	p.w.WriteByte('$')
	if !pe.Short {
		p.w.WriteByte('{')
	}
	if pe.Flags != nil {
		p.w.WriteByte('(')
		p.writeLit(pe.Flags.Value)
		p.w.WriteByte(')')
	}
	switch {
	case pe.Length:
		p.w.WriteByte('#')
	case pe.Width:
		p.w.WriteByte('%')
	case pe.IsSet:
		p.w.WriteByte('+')
	case pe.Excl:
		p.w.WriteByte('!')
	}
	switch {
	case pe.Param != nil:
		p.writeLit(pe.Param.Value)
	case pe.NestedParam != nil:
		// Note that Zsh supports ${${nested}} but not ${$nested},
		// so we need to avoid that simplification here.
		saved := p.minify
		p.minify = false
		p.wordPart(pe.NestedParam, nil)
		p.minify = saved
	}
	p.wroteIndex(pe.Index)
	switch {
	case len(pe.Modifiers) > 0:
		for _, lit := range pe.Modifiers {
			p.w.WriteByte(':')
			p.w.WriteString(lit.Value)
		}
	case pe.Slice != nil:
		p.w.WriteByte(':')
		p.arithmExpr(pe.Slice.Offset, true, true)
		if pe.Slice.Length != nil {
			p.w.WriteByte(':')
			p.arithmExpr(pe.Slice.Length, true, false)
		}
	case pe.Repl != nil:
		if pe.Repl.All {
			p.w.WriteByte('/')
		}
		p.w.WriteByte('/')
		if pe.Repl.Orig != nil {
			p.word(pe.Repl.Orig)
		}
		p.w.WriteByte('/')
		if pe.Repl.With != nil {
			p.word(pe.Repl.With)
		}
	case pe.Names != 0:
		p.writeLit(pe.Names.String())
	case pe.Exp != nil:
		p.w.WriteString(pe.Exp.Op.String())
		if pe.Exp.Word != nil {
			p.word(pe.Exp.Word)
		}
	}
	if !pe.Short {
		p.w.WriteByte('}')
	}
}

func (p *Printer) cmdSubst(cs *CmdSubst) {
	switch {
	case cs.TempFile:
		p.w.WriteString("${")
		p.wantSpace = spaceRequired
		p.nestedStmts(cs.Stmts, cs.Last, cs.Right)
		p.wantSpace = spaceNotRequired
		p.semiRsrv("}", cs.Right)
	case cs.ReplyVar:
		p.w.WriteString("${|")
		p.nestedStmts(cs.Stmts, cs.Last, cs.Right)
		p.wantSpace = spaceNotRequired
		p.semiRsrv("}", cs.Right)
	// Special case: `# inline comment`
	case cs.Backquotes && len(cs.Stmts) == 0 &&
		len(cs.Last) == 1 && cs.Right.Line() == p.line:
		p.w.WriteString("`#")
		p.w.WriteString(cs.Last[0].Text)
		p.w.WriteString("`")
	default:
		p.w.WriteString("$(")
		if len(cs.Stmts) > 0 && startsWithLparen(cs.Stmts[0]) {
			p.wantSpace = spaceRequired
		} else {
			p.wantSpace = spaceNotRequired
		}
		p.nestedStmts(cs.Stmts, cs.Last, cs.Right)
		p.rightParen(cs.Right)
	}
}

func (p *Printer) loop(loop Loop) {
	switch loop := loop.(type) {
	case *WordIter:
		p.writeLit(loop.Name.Value)
		if loop.InPos.IsValid() {
			p.spacedString(" in", Pos{})
			p.wordJoin(loop.Items)
		}
	case *CStyleLoop:
		p.w.WriteString("((")
		if loop.Init == nil {
			p.space()
		}
		p.arithmExpr(loop.Init, false, false)
		p.w.WriteString("; ")
		p.arithmExpr(loop.Cond, false, false)
		p.w.WriteString("; ")
		p.arithmExpr(loop.Post, false, false)
		p.w.WriteString("))")
	}
}

func (p *Printer) arithmExpr(expr ArithmExpr, compact, spacePlusMinus bool) {
	if p.minify {
		compact = true
	}
	p.arithmExprRecurse(expr, compact, spacePlusMinus)
}

func (p *Printer) arithmExprRecurse(expr ArithmExpr, compact, spacePlusMinus bool) {
	switch expr := expr.(type) {
	case *Word:
		p.word(expr)
	case *BinaryArithm:
		if compact {
			p.arithmExprRecurse(expr.X, compact, spacePlusMinus)
			p.w.WriteString(expr.Op.String())
			p.arithmExprRecurse(expr.Y, compact, false)
		} else {
			p.arithmExprRecurse(expr.X, compact, spacePlusMinus)
			if expr.Op != Comma {
				p.space()
			}
			p.w.WriteString(expr.Op.String())
			p.space()
			p.arithmExprRecurse(expr.Y, compact, false)
		}
	case *UnaryArithm:
		if expr.Post {
			p.arithmExprRecurse(expr.X, compact, spacePlusMinus)
			p.w.WriteString(expr.Op.String())
		} else {
			if spacePlusMinus {
				switch expr.Op {
				case Plus, Minus:
					p.space()
				}
			}
			p.w.WriteString(expr.Op.String())
			p.arithmExprRecurse(expr.X, compact, false)
		}
	case *ParenArithm:
		p.w.WriteByte('(')
		p.arithmExprRecurse(expr.X, false, false)
		p.w.WriteByte(')')
	case *FlagsArithm:
		p.w.WriteByte('(')
		p.w.WriteString(expr.Flags.Value)
		p.w.WriteByte(')')
		if expr.X != nil {
			p.arithmExprRecurse(expr.X, compact, false)
		}
	}
}

func (p *Printer) testExpr(expr TestExpr) {
	// Multi-line test expressions don't need to escape newlines.
	if expr.Pos().Line() > p.line {
		p.newlines(expr.Pos())
		p.spacePad(expr.Pos())
	} else if p.wantSpace == spaceRequired {
		p.space()
	}
	p.testExprSameLine(expr)
}

func (p *Printer) testExprSameLine(expr TestExpr) {
	p.advanceLine(expr.Pos().Line())
	switch expr := expr.(type) {
	case *Word:
		p.word(expr)
	case *BinaryTest:
		p.testExprSameLine(expr.X)
		p.space()
		p.w.WriteString(expr.Op.String())
		switch expr.Op {
		case AndTest, OrTest:
			p.wantSpace = spaceRequired
			p.testExpr(expr.Y)
		default:
			p.space()
			p.testExprSameLine(expr.Y)
		}
	case *UnaryTest:
		p.w.WriteString(expr.Op.String())
		p.space()
		p.testExprSameLine(expr.X)
	case *ParenTest:
		p.w.WriteByte('(')
		if startsWithLparen(expr.X) {
			p.wantSpace = spaceRequired
		} else {
			p.wantSpace = spaceNotRequired
		}
		p.testExpr(expr.X)
		p.w.WriteByte(')')
	}
}

func (p *Printer) word(w *Word) {
	p.wordParts(w.Parts, false)
	p.wantSpace = spaceRequired
}

func (p *Printer) unquotedWord(w *Word) {
	for _, wp := range w.Parts {
		switch wp := wp.(type) {
		case *SglQuoted:
			p.writeLit(wp.Value)
		case *DblQuoted:
			p.wordParts(wp.Parts, true)
		case *Lit:
			for i := 0; i < len(wp.Value); i++ {
				if b := wp.Value[i]; b == '\\' {
					if i++; i < len(wp.Value) {
						p.w.WriteByte(wp.Value[i])
					}
				} else {
					p.w.WriteByte(b)
				}
			}
		}
	}
}

func (p *Printer) wordJoin(ws []*Word) {
	anyNewline := false
	for _, w := range ws {
		if pos := w.Pos(); pos.Line() > p.line && !p.singleLine {
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.bslashNewl()
		}
		p.spacePad(w.Pos())
		p.word(w)
	}
	if anyNewline {
		p.decLevel()
	}
}

func (p *Printer) casePatternJoin(pats []*Word) {
	anyNewline := false
	for i, w := range pats {
		// Only valid situation for a literal 'esac' here is with a preceding left paran.
		if i == 0 && w.Lit() == "esac" {
			p.w.WriteString("(")
		}
		if i > 0 {
			p.spacedToken("|", Pos{})
		}
		if p.wantsNewline(w.Pos(), true) {
			if !anyNewline {
				p.incLevel()
				anyNewline = true
			}
			p.bslashNewl()
		} else {
			p.spacePad(w.Pos())
		}
		p.word(w)
	}
	if anyNewline {
		p.decLevel()
	}
}

func (p *Printer) elemJoin(elems []*ArrayElem, last []Comment) {
	p.incLevel()
	for _, el := range elems {
		var left []Comment
		for _, c := range el.Comments {
			if c.Pos().After(el.Pos()) {
				left = append(left, c)
				break
			}
			p.comments(c)
		}
		// Multi-line array expressions don't need to escape newlines.
		if el.Pos().Line() > p.line {
			p.newlines(el.Pos())
			p.spacePad(el.Pos())
		} else if p.wantSpace == spaceRequired {
			p.space()
		}
		if p.wroteIndex(el.Index) {
			p.w.WriteByte('=')
		}
		if el.Value != nil {
			p.word(el.Value)
		}
		p.comments(left...)
	}
	if len(last) > 0 {
		p.comments(last...)
		p.flushComments()
	}
	p.decLevel()
}

func (p *Printer) stmt(s *Stmt) {
	p.wroteSemi = false
	if s.Negated {
		p.spacedString("!", s.Pos())
	}
	var startRedirs int
	if s.Cmd != nil {
		startRedirs = p.command(s.Cmd, s.Redirs)
	}
	p.incLevel()
	for _, r := range s.Redirs[startRedirs:] {
		if p.wantsNewline(r.OpPos, true) {
			p.bslashNewl()
		}
		if p.wantSpace == spaceRequired {
			p.spacePad(r.Pos())
		}
		if r.N != nil {
			p.writeLit(r.N.Value)
		}
		p.w.WriteString(r.Op.String())
		if p.spaceRedirects && (r.Op != DplIn && r.Op != DplOut) {
			p.space()
		} else {
			p.wantSpace = spaceRequired
		}
		p.word(r.Word)
		if r.Op == Hdoc || r.Op == DashHdoc {
			p.pendingHdocs = append(p.pendingHdocs, r)
		}
	}
	sep := s.Semicolon.IsValid() && s.Semicolon.Line() > p.line && !p.singleLine
	if sep || s.Background || s.Coprocess || s.Disown {
		if sep {
			p.bslashNewl()
		} else if !p.minify {
			p.space()
		}
		if s.Background {
			p.w.WriteString("&")
		} else if s.Coprocess {
			p.w.WriteString("|&")
		} else if s.Disown {
			p.w.WriteString("&|")
		} else {
			p.w.WriteString(";")
		}
		p.wroteSemi = true
		p.wantSpace = spaceRequired
	}
	p.decLevel()
}

func (p *Printer) printRedirsUntil(redirs []*Redirect, startRedirs int, pos Pos) int {
	for _, r := range redirs[startRedirs:] {
		if r.Pos().After(pos) || r.Op == Hdoc || r.Op == DashHdoc {
			break
		}
		if p.wantSpace == spaceRequired {
			p.spacePad(r.Pos())
		}
		if r.N != nil {
			p.writeLit(r.N.Value)
		}
		p.w.WriteString(r.Op.String())
		if p.spaceRedirects && (r.Op != DplIn && r.Op != DplOut) {
			p.space()
		} else {
			p.wantSpace = spaceRequired
		}
		p.word(r.Word)
		startRedirs++
	}
	return startRedirs
}

func (p *Printer) command(cmd Command, redirs []*Redirect) (startRedirs int) {
	p.advanceLine(cmd.Pos().Line())
	p.spacePad(cmd.Pos())
	switch cmd := cmd.(type) {
	case *CallExpr:
		p.assigns(cmd.Assigns)
		if len(cmd.Args) > 0 {
			startRedirs = p.printRedirsUntil(redirs, startRedirs, cmd.Args[0].Pos())
		}
		if len(cmd.Args) <= 1 {
			p.wordJoin(cmd.Args)
			return startRedirs
		}
		p.wordJoin(cmd.Args[:1])
		startRedirs = p.printRedirsUntil(redirs, startRedirs, cmd.Args[1].Pos())
		p.wordJoin(cmd.Args[1:])
	case *Block:
		p.w.WriteByte('{')
		// avoid ; in an empty block
		p.wroteSemi = true
		p.wantSpace = spaceRequired
		// Forbid "foo()\n{ bar; }"
		p.wantNewline = p.wantNewline || p.funcNextLine
		p.nestedStmts(cmd.Stmts, cmd.Last, cmd.Rbrace)
		p.semiRsrv("}", cmd.Rbrace)
	case *IfClause:
		p.ifClause(cmd, false)
	case *Subshell:
		p.w.WriteByte('(')
		stmts := cmd.Stmts
		if len(stmts) > 0 && startsWithLparen(stmts[0]) {
			p.wantSpace = spaceRequired
			// Add a space between nested parentheses if we're printing them in a single line,
			// to avoid the ambiguity between `((` and `( (`.
			if (cmd.Lparen.Line() != stmts[0].Pos().Line() || len(stmts) > 1) && !p.singleLine {
				p.wantSpace = spaceNotRequired

				if p.minify {
					p.mustNewline = true
				}
			}
		} else if len(stmts) == 0 {
			// Zsh allows empty subshells, but prevent `()`
			// from looking like `() { anon-func; }`.
			p.wantSpace = spaceRequired
		} else {
			p.wantSpace = spaceNotRequired
		}

		p.spacePad(stmtsPos(cmd.Stmts, cmd.Last))
		p.nestedStmts(cmd.Stmts, cmd.Last, cmd.Rparen)
		p.wantSpace = spaceNotRequired
		p.spacePad(cmd.Rparen)
		p.rightParen(cmd.Rparen)
	case *WhileClause:
		if cmd.Until {
			p.spacedString("until", cmd.Pos())
		} else {
			p.spacedString("while", cmd.Pos())
		}
		p.nestedStmts(cmd.Cond, cmd.CondLast, Pos{})
		p.semiOrNewl("do", cmd.DoPos)
		p.nestedStmts(cmd.Do, cmd.DoLast, cmd.DonePos)
		p.semiRsrv("done", cmd.DonePos)
	case *ForClause:
		if cmd.Select {
			p.w.WriteString("select ")
		} else {
			p.w.WriteString("for ")
		}
		p.loop(cmd.Loop)
		p.semiOrNewl("do", cmd.DoPos)
		p.nestedStmts(cmd.Do, cmd.DoLast, cmd.DonePos)
		p.semiRsrv("done", cmd.DonePos)
	case *BinaryCmd:
		p.stmt(cmd.X)
		if p.minify || p.singleLine || cmd.Y.Pos().Line() <= p.line {
			// leave p.nestedBinary untouched
			p.spacedToken(cmd.Op.String(), cmd.OpPos)
			p.advanceLine(cmd.Y.Pos().Line())
			p.stmt(cmd.Y)
			break
		}
		indent := !p.nestedBinary
		if indent {
			p.incLevel()
		}
		if p.binNextLine {
			if len(p.pendingHdocs) == 0 {
				p.bslashNewl()
			}
			p.spacedToken(cmd.Op.String(), cmd.OpPos)
			if len(cmd.Y.Comments) > 0 {
				p.wantSpace = spaceNotRequired
				p.newline(cmd.Y.Pos())
				p.indent()
				p.comments(cmd.Y.Comments...)
				p.newline(Pos{})
				p.indent()
			}
		} else {
			p.spacedToken(cmd.Op.String(), cmd.OpPos)
			p.advanceLine(cmd.OpPos.Line())
			p.comments(cmd.Y.Comments...)
			p.newline(Pos{})
			p.indent()
		}
		p.advanceLine(cmd.Y.Pos().Line())
		_, p.nestedBinary = cmd.Y.Cmd.(*BinaryCmd)
		p.stmt(cmd.Y)
		if indent {
			p.decLevel()
		}
		p.nestedBinary = false
	case *FuncDecl:
		if cmd.RsrvWord {
			p.spacedString("function", Pos{})
		}
		if cmd.Name != nil {
			p.spacedString(cmd.Name.Value, Pos{})
		} else {
			for _, name := range cmd.Names {
				p.spacedString(name.Value, Pos{})
			}
		}
		if !cmd.RsrvWord || cmd.Parens {
			p.w.WriteString("()")
			p.wantSpace = spaceNotRequired
		}
		if p.funcNextLine {
			p.newline(Pos{})
			p.indent()
		} else if !cmd.Parens || !p.minify {
			p.space()
		}
		p.advanceLine(cmd.Body.Pos().Line())
		p.comments(cmd.Body.Comments...)
		p.stmt(cmd.Body)
	case *CaseClause:
		p.w.WriteString("case ")
		p.word(cmd.Word)
		p.w.WriteString(" in")
		p.advanceLine(cmd.In.Line())
		p.wantSpace = spaceRequired
		if p.swtCaseIndent {
			p.incLevel()
		}
		if len(cmd.Items) == 0 {
			// Apparently "case x in; esac" is invalid shell.
			p.mustNewline = true
		}
		for i, ci := range cmd.Items {
			var last []Comment
			for i, c := range ci.Comments {
				if c.Pos().After(ci.Pos()) {
					last = ci.Comments[i:]
					break
				}
				p.comments(c)
			}
			p.newlines(ci.Pos())
			p.spacePad(ci.Pos())
			p.casePatternJoin(ci.Patterns)
			p.w.WriteByte(')')
			if !p.minify {
				p.wantSpace = spaceRequired
			} else {
				p.wantSpace = spaceNotRequired
			}

			p.nestedStmts(ci.Stmts, ci.Last, ci.OpPos)
			p.level++
			if !p.minify || i != len(cmd.Items)-1 {
				if p.wantsNewline(ci.OpPos, false) {
					p.newlines(ci.OpPos)
					p.wantNewline = true
				}
				p.spacedToken(ci.Op.String(), ci.OpPos)
				p.advanceLine(ci.OpPos.Line())
				// avoid ; directly after tokens like ;;
				p.wroteSemi = true
			}
			p.comments(last...)
			p.flushComments()
			p.level--
		}
		p.comments(cmd.Last...)
		if p.swtCaseIndent {
			p.flushComments()
			p.decLevel()
		}
		p.semiRsrv("esac", cmd.Esac)
	case *ArithmCmd:
		p.w.WriteString("((")
		if cmd.Unsigned {
			p.w.WriteString("# ")
		}
		p.arithmExpr(cmd.X, false, false)
		p.w.WriteString("))")
	case *TestClause:
		p.w.WriteString("[[ ")
		p.incLevel()
		p.testExpr(cmd.X)
		p.decLevel()
		p.spacedString("]]", cmd.Right)
	case *DeclClause:
		p.spacedString(cmd.Variant.Value, cmd.Pos())
		p.assigns(cmd.Args)
	case *TimeClause:
		p.spacedString("time", cmd.Pos())
		if cmd.PosixFormat {
			p.spacedString("-p", cmd.Pos())
		}
		if cmd.Stmt != nil {
			p.stmt(cmd.Stmt)
		}
	case *CoprocClause:
		p.spacedString("coproc", cmd.Pos())
		if cmd.Name != nil {
			p.space()
			p.word(cmd.Name)
		}
		p.space()
		p.stmt(cmd.Stmt)
	case *LetClause:
		p.spacedString("let", cmd.Pos())
		for _, n := range cmd.Exprs {
			p.space()
			p.arithmExpr(n, true, false)
		}
	case *TestDecl:
		p.spacedString("@test", cmd.Pos())
		p.space()
		p.word(cmd.Description)
		p.space()
		p.stmt(cmd.Body)
	default:
		panic(fmt.Sprintf("syntax.Printer: unexpected node type %T", cmd))
	}
	return startRedirs
}

func (p *Printer) ifClause(ic *IfClause, elif bool) {
	if !elif {
		p.spacedString("if", ic.Pos())
	}
	p.nestedStmts(ic.Cond, ic.CondLast, Pos{})
	p.semiOrNewl("then", ic.ThenPos)
	thenEnd := ic.FiPos
	el := ic.Else
	if el != nil {
		thenEnd = el.Position
	}
	p.nestedStmts(ic.Then, ic.ThenLast, thenEnd)

	if el != nil && el.ThenPos.IsValid() {
		p.comments(ic.Last...)
		p.semiRsrv("elif", el.Position)
		p.ifClause(el, true)
		return
	}
	if el == nil {
		p.comments(ic.Last...)
	} else {
		var left []Comment
		for _, c := range ic.Last {
			if c.Pos().After(el.Position) {
				left = append(left, c)
				break
			}
			p.comments(c)
		}
		p.semiRsrv("else", el.Position)
		p.comments(left...)
		p.nestedStmts(el.Then, el.ThenLast, ic.FiPos)
		p.comments(el.Last...)
	}
	p.semiRsrv("fi", ic.FiPos)
}

func (p *Printer) stmtList(stmts []*Stmt, last []Comment) {
	sep := p.wantNewline || (len(stmts) > 0 && stmts[0].Pos().Line() > p.line)
	for i, s := range stmts {
		if i > 0 && p.singleLine && p.wantNewline && !p.wroteSemi {
			// In singleLine mode, ensure we use semicolons between
			// statements.
			p.w.WriteByte(';')
			p.wantSpace = spaceRequired
		}
		pos := s.Pos()
		var midComs, endComs []Comment
		for _, c := range s.Comments {
			// Comments after the end of this command. Note that
			// this includes "<<EOF # comment".
			if s.Cmd != nil && c.End().After(s.Cmd.End()) {
				endComs = append(endComs, c)
				break
			}
			// Comments between the beginning of the statement and
			// the end of the command.
			if c.Pos().After(pos) {
				midComs = append(midComs, c)
				continue
			}
			// The rest of the comments are before the entire
			// statement.
			p.comments(c)
		}
		if p.mustNewline || !p.minify || p.wantSpace == spaceRequired {
			p.newlines(pos)
		}
		p.advanceLine(pos.Line())
		p.comments(midComs...)
		p.stmt(s)
		p.comments(endComs...)
		p.wantNewline = true
	}
	if len(stmts) == 1 && !sep {
		p.wantNewline = false
	}
	p.comments(last...)
}

func (p *Printer) nestedStmts(stmts []*Stmt, last []Comment, closing Pos) {
	p.incLevel()
	switch {
	case len(stmts) > 1:
		// Force a newline if we find:
		//     { stmt; stmt; }
		p.wantNewline = true
	case closing.Line() > p.line && len(stmts) > 0 &&
		stmtsEnd(stmts, last).Line() < closing.Line():
		// Force a newline if we find:
		//     { stmt
		//     }
		p.wantNewline = true
	case len(p.pendingComments) > 0 && len(stmts) > 0:
		// Force a newline if we find:
		//     for i in a b # stmt
		//     do foo; done
		p.wantNewline = true
	}
	p.stmtList(stmts, last)
	if closing.IsValid() {
		p.flushComments()
	}
	p.decLevel()
}

func (p *Printer) assigns(assigns []*Assign) {
	p.incLevel()
	for _, a := range assigns {
		if p.wantsNewline(a.Pos(), true) {
			p.bslashNewl()
		} else {
			p.spacePad(a.Pos())
		}
		if a.Name != nil {
			p.writeLit(a.Name.Value)
			p.wroteIndex(a.Index)
			if a.Append {
				p.w.WriteByte('+')
			}
			if !a.Naked {
				p.w.WriteByte('=')
			}
		}
		if a.Value != nil {
			// Ensure we don't use an escaped newline after '=',
			// because that can result in indentation, thus
			// splitting "foo=bar" into "foo= bar".
			p.advanceLine(a.Value.Pos().Line())
			p.word(a.Value)
		} else if a.Array != nil {
			p.wantSpace = spaceNotRequired
			p.w.WriteByte('(')
			p.elemJoin(a.Array.Elems, a.Array.Last)
			p.rightParen(a.Array.Rparen)
		}
		p.wantSpace = spaceRequired
	}
	p.decLevel()
}

type wantSpaceState uint8

const (
	spaceNotRequired wantSpaceState = iota
	spaceRequired                   // we should generally print a space or a newline next
	spaceWritten                    // we have just written a space or newline
)

// extraIndenter ensures that all lines in a '<<-' heredoc body have at least
// baseIndent leading tabs. Those that had more tab indentation than the first
// heredoc line will keep that relative indentation.
type extraIndenter struct {
	bufWriter
	baseIndent int

	firstIndent int
	firstChange int
	curLine     []byte
}

func (e *extraIndenter) WriteByte(b byte) error {
	e.curLine = append(e.curLine, b)
	if b != '\n' {
		return nil
	}
	trimmed := bytes.TrimLeft(e.curLine, "\t")
	if len(trimmed) == 1 {
		// no tabs if this is an empty line, i.e. "\n"
		e.bufWriter.Write(trimmed)
		e.curLine = e.curLine[:0]
		return nil
	}

	lineIndent := len(e.curLine) - len(trimmed)
	if e.firstIndent < 0 {
		// This is the first heredoc line we add extra indentation to.
		// Keep track of how much we indented.
		e.firstIndent = lineIndent
		e.firstChange = e.baseIndent - lineIndent
		lineIndent = e.baseIndent

	} else if lineIndent < e.firstIndent {
		// This line did not have enough indentation; simply indent it
		// like the first line.
		lineIndent = e.firstIndent
	} else {
		// This line had plenty of indentation. Add the extra
		// indentation that the first line had, for consistency.
		lineIndent += e.firstChange
	}
	e.bufWriter.WriteByte(tabwriter.Escape)
	for range lineIndent {
		e.bufWriter.WriteByte('\t')
	}
	e.bufWriter.WriteByte(tabwriter.Escape)
	e.bufWriter.Write(trimmed)
	e.curLine = e.curLine[:0]
	return nil
}

func (e *extraIndenter) WriteString(s string) (int, error) {
	for i := range len(s) {
		e.WriteByte(s[i])
	}
	return len(s), nil
}

func startsWithLparen(node Node) bool {
	switch node := node.(type) {
	case *Stmt:
		return startsWithLparen(node.Cmd)
	case *BinaryCmd:
		return startsWithLparen(node.X)
	case *Subshell:
		return true // keep ( (
	case *ArithmCmd:
		return true // keep ( ((
	}
	return false
}
