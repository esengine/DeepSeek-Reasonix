package evidence

import (
	"path/filepath"
	"strconv"
	"strings"
)

// isPytestModule is pytest's default python_files contract: test_*.py and
// *_test.py. A project that reconfigures collection is read as having none.
func isPytestModule(path string) bool {
	base := filepath.Base(filepath.ToSlash(path))
	return strings.HasSuffix(base, ".py") && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py"))
}

// pytestBodies maps each test pytest collects by default — module-level test*
// functions and test* methods of module-level Test* classes — to a signature of
// its decorators and body. ok is false when the source does not lex, so a file
// mid-edit is never read as having lost its tests.
func pytestBodies(src string) (map[string]string, bool) {
	lines, ok := pyLogicalLines(src)
	if !ok {
		return nil, false
	}
	bodies := map[string]string{}
	var decorators []pyLine
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.depth != 0 {
			continue
		}
		if l.toks[0] == "@" {
			decorators = append(decorators, l)
			continue
		}
		end := pyBlockEnd(lines, i)
		if name, ok := pyDefName(l.toks); ok && strings.HasPrefix(name, "test") {
			bodies[name] = pySignature(decorators, lines[i:end])
		} else if class, ok := pyClassName(l.toks); ok && strings.HasPrefix(class, "Test") {
			pytestMethods(bodies, class, lines[i+1:end])
		}
		decorators = nil
		i = end - 1
	}
	return bodies, true
}

func pytestMethods(bodies map[string]string, class string, body []pyLine) {
	var decorators []pyLine
	for i := 0; i < len(body); i++ {
		l := body[i]
		if l.depth != 1 {
			continue
		}
		if l.toks[0] == "@" {
			decorators = append(decorators, l)
			continue
		}
		end := pyBlockEnd(body, i)
		if name, ok := pyDefName(l.toks); ok && strings.HasPrefix(name, "test") {
			bodies[class+"::"+name] = pySignature(decorators, body[i:end])
		}
		decorators = nil
		i = end - 1
	}
}

func pyDefName(toks []string) (string, bool) {
	if len(toks) > 2 && toks[0] == "async" {
		toks = toks[1:]
	}
	if len(toks) > 1 && toks[0] == "def" {
		return toks[1], true
	}
	return "", false
}

func pyClassName(toks []string) (string, bool) {
	if len(toks) > 1 && toks[0] == "class" {
		return toks[1], true
	}
	return "", false
}

func pyBlockEnd(lines []pyLine, i int) int {
	j := i + 1
	for j < len(lines) && lines[j].depth > lines[i].depth {
		j++
	}
	return j
}

// pySignature is the token shape of a test with depths relative to its def, so
// reindenting a class cannot read as rewriting its tests. A leading docstring
// is left out: it is a statement Python evaluates and discards.
func pySignature(decorators, block []pyLine) string {
	base := block[0].depth
	var b strings.Builder
	write := func(l pyLine) {
		b.WriteString(strconv.Itoa(l.depth - base))
		b.WriteByte('|')
		b.WriteString(strings.Join(pyDropTrailingCommas(l.toks), "\x00"))
		b.WriteByte('\n')
	}
	for _, d := range decorators {
		write(d)
	}
	write(block[0])
	for k, l := range block[1:] {
		if k == 0 && l.depth == base+1 && pyAllStrings(l.toks) {
			continue
		}
		write(l)
	}
	return b.String()
}

// pyDropTrailingCommas removes the commas a formatter adds or strips at will:
// one before ] or }, before the ) of a call, or before a ) whose parentheses
// already hold another top-level comma. (x,) is a tuple and (x) is not.
func pyDropTrailingCommas(toks []string) []string {
	type group struct {
		commas int
		call   bool
	}
	drop := map[int]bool{}
	var open []group
	for i, t := range toks {
		switch t {
		case "(", "[", "{":
			call := t == "(" && i > 0 && (toks[i-1] == ")" || toks[i-1] == "]" || pyIsName(toks[i-1]) && !pyKeywords[toks[i-1]])
			open = append(open, group{call: call})
		case ",":
			if len(open) > 0 {
				open[len(open)-1].commas++
			}
		case ")", "]", "}":
			if len(open) == 0 {
				continue
			}
			g := open[len(open)-1]
			open = open[:len(open)-1]
			if i > 0 && toks[i-1] == "," && (t != ")" || g.call || g.commas > 1) {
				drop[i-1] = true
			}
		}
	}
	if len(drop) == 0 {
		return toks
	}
	out := make([]string, 0, len(toks)-len(drop))
	for i, t := range toks {
		if !drop[i] {
			out = append(out, t)
		}
	}
	return out
}

func pyIsName(t string) bool {
	return pyNameByte(t[0]) && !strings.ContainsAny(t, `'"`)
}

// pyKeywords is keyword.kwlist: a ( after one of these opens a tuple or a
// group, never a call.
var pyKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true,
	"async": true, "await": true, "break": true, "class": true, "continue": true, "def": true,
	"del": true, "elif": true, "else": true, "except": true, "finally": true, "for": true,
	"from": true, "global": true, "if": true, "import": true, "in": true, "is": true,
	"lambda": true, "nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
}

func pyAllStrings(toks []string) bool {
	for _, t := range toks {
		if t = strings.TrimLeft(t, "rRbBuUfFtT"); t == "" || (t[0] != '\'' && t[0] != '"') {
			return false
		}
	}
	return true
}

// pyLine is one logical line: its block depth from the indentation stack and
// its tokens. Comments and whitespace between tokens are not tokens.
type pyLine struct {
	depth int
	toks  []string
}

// pyLogicalLines lexes Python's line structure: indentation measured the way
// the tokenizer measures it, lines joined inside brackets and after a trailing
// backslash, strings kept whole. It rejects what Python would reject about that
// structure — an unclosed string or bracket, a dedent to no enclosing level.
func pyLogicalLines(src string) ([]pyLine, bool) {
	lx := pyLexer{src: src, stack: []int{0}}
	return lx.run()
}

type pyLexer struct {
	src     string
	i       int
	stack   []int
	lines   []pyLine
	toks    []string
	indent  int
	bracket int
}

func (lx *pyLexer) run() ([]pyLine, bool) {
	lineStart := true
	for lx.i < len(lx.src) {
		if lineStart && lx.bracket == 0 {
			lineStart = false
			if !lx.measureIndent() {
				lineStart = true
				continue
			}
		}
		ended, ok := lx.step()
		if !ok {
			return nil, false
		}
		lineStart = lineStart || ended
	}
	if lx.bracket != 0 || !lx.endLine() {
		return nil, false
	}
	return lx.lines, true
}

// step consumes what separates tokens, or one token. ended reports a logical
// line closed by a newline outside brackets.
func (lx *pyLexer) step() (ended, ok bool) {
	c := lx.src[lx.i]
	switch {
	case c == ' ' || c == '\t' || c == '\f':
		lx.i++
	case c == '#':
		for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
			lx.i++
		}
	case c == '\\' && lx.i+1 < len(lx.src) && (lx.src[lx.i+1] == '\n' || lx.src[lx.i+1] == '\r'):
		lx.i += 2
		if lx.src[lx.i-1] == '\r' && lx.i < len(lx.src) && lx.src[lx.i] == '\n' {
			lx.i++
		}
	case c == '\n' || c == '\r':
		lx.i++
		if lx.bracket == 0 {
			return true, lx.endLine()
		}
	default:
		return false, lx.token(c)
	}
	return false, true
}

func (lx *pyLexer) token(c byte) bool {
	switch {
	case c == '\'' || c == '"':
		return lx.lexString(lx.i)
	case pyNameByte(c):
		start := lx.i
		lx.scan(func(b byte) bool { return pyNameByte(b) || isDigit(b) })
		if lx.i < len(lx.src) && (lx.src[lx.i] == '\'' || lx.src[lx.i] == '"') && pyStringPrefix(lx.src[start:lx.i]) {
			return lx.lexString(start)
		}
		lx.toks = append(lx.toks, lx.src[start:lx.i])
	case isDigit(c):
		start := lx.i
		lx.scan(func(b byte) bool { return pyNameByte(b) || isDigit(b) || b == '.' })
		lx.toks = append(lx.toks, lx.src[start:lx.i])
	default:
		switch c {
		case '(', '[', '{':
			lx.bracket++
		case ')', ']', '}':
			if lx.bracket--; lx.bracket < 0 {
				return false
			}
		}
		lx.toks = append(lx.toks, string(c))
		lx.i++
	}
	return true
}

func (lx *pyLexer) scan(in func(byte) bool) {
	for lx.i < len(lx.src) && in(lx.src[lx.i]) {
		lx.i++
	}
}

// measureIndent reads the leading whitespace of a physical line. A line holding
// only whitespace or a comment carries no indentation and is consumed whole.
func (lx *pyLexer) measureIndent() bool {
	col := 0
	for ; lx.i < len(lx.src) && strings.IndexByte(" \t\f", lx.src[lx.i]) >= 0; lx.i++ {
		switch lx.src[lx.i] {
		case ' ':
			col++
		case '\t':
			col = (col/8 + 1) * 8
		default:
			col = 0
		}
	}
	if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' || lx.src[lx.i] == '\r' || lx.src[lx.i] == '#' {
		for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
			lx.i++
		}
		if lx.i < len(lx.src) {
			lx.i++
		}
		return false
	}
	lx.indent = col
	return true
}

func (lx *pyLexer) endLine() bool {
	if len(lx.toks) == 0 {
		return true
	}
	top := lx.stack[len(lx.stack)-1]
	switch {
	case lx.indent > top:
		lx.stack = append(lx.stack, lx.indent)
	case lx.indent < top:
		for len(lx.stack) > 1 && lx.stack[len(lx.stack)-1] > lx.indent {
			lx.stack = lx.stack[:len(lx.stack)-1]
		}
		if lx.stack[len(lx.stack)-1] != lx.indent {
			return false
		}
	}
	lx.lines = append(lx.lines, pyLine{depth: len(lx.stack) - 1, toks: lx.toks})
	lx.toks = nil
	return true
}

// lexString consumes a string literal whose prefix begins at start. A backslash
// always shields the next byte from closing it, raw strings included.
func (lx *pyLexer) lexString(start int) bool {
	q := lx.src[lx.i]
	triple := strings.HasPrefix(lx.src[lx.i:], strings.Repeat(string(q), 3))
	if triple {
		lx.i += 3
	} else {
		lx.i++
	}
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == '\\':
			lx.i += 2
			continue
		case !triple && (c == '\n' || c == '\r'):
			return false
		case c == q && (!triple || strings.HasPrefix(lx.src[lx.i:], strings.Repeat(string(q), 3))):
			if triple {
				lx.i += 3
			} else {
				lx.i++
			}
			lx.toks = append(lx.toks, lx.src[start:lx.i])
			return true
		}
		lx.i++
	}
	return false
}

func pyStringPrefix(s string) bool {
	switch strings.ToLower(s) {
	case "r", "u", "b", "f", "t", "br", "rb", "fr", "rf", "tr", "rt":
		return true
	}
	return false
}

func pyNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
