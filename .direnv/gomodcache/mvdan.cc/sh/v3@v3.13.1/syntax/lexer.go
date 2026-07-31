// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"io"
	"unicode/utf8"
)

func asciiLetter[T rune | byte](r T) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}

func asciiDigit[T rune | byte](r T) bool {
	return r >= '0' && r <= '9'
}

// bytes that form or start a token
func regOps(r rune) bool {
	switch r {
	case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
		return true
	}
	return false
}

// tokenize these inside parameter expansions
func paramOps(r rune) bool {
	switch r {
	case '}', '#', '!', ':', '-', '+', '=', '?', '%', '[', ']', '/', '^',
		',', '@', '*':
		return true
	}
	return false
}

// tokenize these inside arithmetic expansions
func arithmOps(r rune) bool {
	switch r {
	case '+', '-', '!', '~', '*', '/', '%', '(', ')', '^', '<', '>', ':', '=',
		',', '?', '|', '&', '[', ']', '#', '.':
		return true
	}
	return false
}

func bquoteEscaped(b byte) bool {
	switch b {
	case '$', '`', '\\':
		return true
	}
	return false
}

const escNewl rune = utf8.RuneSelf + 1

func (p *Parser) rune() rune {
	if p.r == '\n' || p.r == escNewl {
		// p.r instead of b so that newline
		// character positions don't have col 0.
		p.line++
		p.col = 0
	}
	p.col += int64(p.w)
	bquotes := 0
retry:
	if p.bsp >= uint(len(p.bs)) && p.fill() == 0 {
		if len(p.bs) == 0 {
			// Necessary for the last position to be correct.
			// TODO: this is not exactly intuitive; figure out a better way.
			p.bsp = 1
		}
		p.r = utf8.RuneSelf
		p.w = 1
		return p.r
	}
	if b := p.bs[p.bsp]; b < utf8.RuneSelf {
		p.bsp++
		switch b {
		case '\x00':
			// Ignore null bytes while parsing, like bash.
			p.col++
			goto retry
		case '\r':
			if p.peek() == '\n' { // \r\n turns into \n
				p.col++
				goto retry
			}
		case '\\':
			if p.r == '\\' {
			} else if p.peek() == '\n' {
				p.bsp++
				p.w, p.r = 1, escNewl
				return escNewl
			} else if p1, p2 := p.peekTwo(); p1 == '\r' && p2 == '\n' { // \\\r\n turns into \\\n
				p.col++
				p.bsp += 2
				p.w, p.r = 2, escNewl
				return escNewl
			}
			// TODO: why is this necessary to ensure correct position info?
			p.readEOF = false
			if p.openBquotes > 0 && bquotes < p.openBquotes &&
				p.bsp < uint(len(p.bs)) && bquoteEscaped(p.bs[p.bsp]) {
				// We turn backquote command substitutions into $(),
				// so we remove the extra backslashes needed by the backquotes.
				bquotes++
				p.col++
				goto retry
			}
		}
		if b == '`' {
			p.lastBquoteEsc = bquotes
		}
		if p.litBs != nil {
			p.litBs = append(p.litBs, b)
		}
		p.w, p.r = 1, rune(b)
		return p.r
	}
decodeRune:
	var w int
	p.r, w = utf8.DecodeRune(p.bs[p.bsp:])
	if p.r == utf8.RuneError && !utf8.FullRune(p.bs[p.bsp:]) {
		// we need more bytes to read a full non-ascii rune
		if p.fill() > 0 {
			goto decodeRune
		}
	}
	if p.litBs != nil {
		p.litBs = append(p.litBs, p.bs[p.bsp:p.bsp+uint(w)]...)
	}
	p.bsp += uint(w)
	if p.r == utf8.RuneError && w == 1 {
		p.posErr(p.nextPos(), "invalid UTF-8 encoding")
	}
	p.w = w
	return p.r
}

// fill reads more bytes from the input src into readBuf.
// Any bytes that had not yet been used at the end of the buffer
// are slid into the beginning of the buffer.
// The number of read bytes is returned, which is at least one
// unless a read error occurred, such as [io.EOF].
func (p *Parser) fill() (n int) {
	if p.readEOF || p.r == utf8.RuneSelf {
		// If the reader already gave us [io.EOF], do not try again.
		// If we decided to stop for any reason, do not bother reading either.
		return 0
	}
	p.offs += int64(p.bsp)
	left := len(p.bs) - int(p.bsp)
	copy(p.readBuf[:left], p.readBuf[p.bsp:])
readAgain:
	n, err := 0, p.readErr
	if err == nil {
		n, err = p.src.Read(p.readBuf[left:])
		p.readErr = err
		if err == io.EOF {
			p.readEOF = true
		}
	}
	if n == 0 {
		if err == nil {
			goto readAgain
		}
		// don't use p.errPass as we don't want to overwrite p.tok
		if err != io.EOF {
			p.err = err
		}
		if left > 0 {
			p.bs = p.readBuf[:left]
		} else {
			p.bs = nil
		}
	} else {
		p.bs = p.readBuf[:left+n]
	}
	p.bsp = 0
	return n
}

func (p *Parser) nextKeepSpaces() {
	r := p.r
	if p.quote != hdocBody && p.quote != hdocBodyTabs {
		// Heredocs handle escaped newlines in a special way, but others do not.
		for r == escNewl {
			r = p.rune()
		}
	}
	p.pos = p.nextPos()
	switch p.quote {
	case runeByRune:
		p.tok = illegalTok
	case dblQuotes:
		switch r {
		case '`', '"', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitDquote(r)
		}
	case hdocBody, hdocBodyTabs:
		switch r {
		case '`', '$':
			p.tok = p.dqToken(r)
		default:
			p.advanceLitHdoc(r)
		}
	case paramExpRepl:
		if r == '/' {
			p.rune()
			p.tok = slash
			break
		}
		fallthrough
	case paramExpExp:
		switch r {
		case '}':
			p.tok = p.paramToken(r)
		case '`', '"', '$', '\'':
			p.tok = p.regToken(r)
		default:
			p.advanceLitOther(r)
		}
	}
	if p.err != nil {
		p.tok = _EOF
	}
}

func (p *Parser) next() {
	if p.r == utf8.RuneSelf {
		p.tok = _EOF
		return
	}
	p.spaced = false
	if p.quote&allKeepSpaces != 0 {
		p.nextKeepSpaces()
		return
	}
	r := p.r
	for r == escNewl {
		r = p.rune()
	}
skipSpace:
	for {
		switch r {
		case utf8.RuneSelf:
			p.tok = _EOF
			return
		case escNewl:
			r = p.rune()
		case ' ', '\t', '\r':
			p.spaced = true
			r = p.rune()
		case '\n':
			if p.tok == _Newl {
				// merge consecutive newline tokens
				r = p.rune()
				continue
			}
			p.spaced = true
			p.tok = _Newl
			if p.quote != hdocWord && len(p.heredocs) > p.buriedHdocs {
				p.doHeredocs()
			}
			return
		default:
			break skipSpace
		}
	}
	if p.stopAt != nil && (p.spaced || p.tok == illegalTok || p.stopToken()) {
		w := utf8.RuneLen(r)
		if bytes.HasPrefix(p.bs[p.bsp-uint(w):], p.stopAt) {
			p.r = utf8.RuneSelf
			p.w = 1
			p.tok = _EOF
			return
		}
	}
	p.pos = p.nextPos()
	switch {
	case p.quote&allRegTokens != 0:
		switch r {
		case ';', '"', '\'', '(', ')', '$', '|', '&', '>', '<', '`':
			if r == '<' && p.lang.in(LangZsh) && p.zshNumRange() {
				p.advanceLitNone(r)
				return
			}
			p.tok = p.regToken(r)
		case '#':
			// If we're parsing $foo#bar, ${foo}#bar, 'foo'#bar, or "foo"#bar,
			// #bar is a continuation of the same word, not a comment.
			if p.quote == unquotedWordCont && !p.spaced {
				p.advanceLitNone(r)
				return
			}
			r = p.rune()
			p.newLit(r)
		runeLoop:
			for {
				switch r {
				case '\n', utf8.RuneSelf:
					break runeLoop
				case escNewl:
					p.litBs = append(p.litBs, '\\', '\n')
					break runeLoop
				case '`':
					if p.backquoteEnd() {
						break runeLoop
					}
				}
				r = p.rune()
			}
			if p.keepComments {
				*p.curComs = append(*p.curComs, Comment{
					Hash: p.pos,
					Text: p.endLit(),
				})
			} else {
				p.litBs = nil
			}
			p.next()
		case '[':
			if p.quote == arrayElems {
				p.rune()
				p.tok = leftBrack
			} else {
				p.advanceLitNone(r)
			}
		case '=':
			if p.peek() == '(' {
				p.rune()
				p.rune()
				p.tok = assgnParen
			} else if p.quote == arrayElems {
				p.rune()
				p.tok = assgn
			} else {
				p.advanceLitNone(r)
			}
		case '?', '*', '+', '@', '!':
			if p.extendedGlob() {
				switch r {
				case '?':
					p.tok = globQuest
				case '*':
					p.tok = globStar
				case '+':
					p.tok = globPlus
				case '@':
					p.tok = globAt
				case '!':
					p.tok = globExcl
				}
				p.rune()
				p.rune()
			} else {
				p.advanceLitNone(r)
			}
		default:
			p.advanceLitNone(r)
		}
	case p.quote&allArithmExpr != 0 && arithmOps(r):
		p.tok = p.arithmToken(r)
	case p.quote&allParamExp != 0 && paramOps(r):
		p.tok = p.paramToken(r)
	case p.quote == testExprRegexp:
		if !p.rxFirstPart && p.spaced {
			p.quote = testExpr
			goto skipSpace
		}
		p.rxFirstPart = false
		switch r {
		case ';', '"', '\'', '$', '&', '>', '<', '`':
			p.tok = p.regToken(r)
		case ')':
			if p.rxOpenParens > 0 {
				// continuation of open paren
				p.advanceLitRe(r)
			} else {
				p.tok = rightParen
				p.quote = testExpr
				p.rune() // we are tokenizing manually
			}
		default: // including '(', '|'
			p.advanceLitRe(r)
		}
	case regOps(r):
		p.tok = p.regToken(r)
	default:
		p.advanceLitOther(r)
	}
	if p.err != nil {
		p.tok = _EOF
	}
}

// extendedGlob determines whether we're parsing a Bash extended globbing expression.
// For example, whether `*` or `@` are followed by `(` to form `@(foo)`.
func (p *Parser) extendedGlob() bool {
	if p.lang.in(LangZsh) {
		// Zsh supports Bash extended globs via the KSH_GLOB option.
		// In Bash we would parse extended globs as [ExtGlob] nodes,
		// but trying to do that in Zsh would cause ambiguity with glob qualifiers.
		// Just like glob qualifiers, parse extended globs as literals in Zsh.
		return false
	}
	if p.val == "function" {
		// We don't support e.g. `function @() { ... }` at the moment, but we could.
		return false
	}
	if p.peek() == '(' {
		// NOTE: empty pattern list is a valid globbing syntax like `@()`,
		// but we'll operate on the "likelihood" that it is a function;
		// only tokenize if its a non-empty pattern list.
		// We do this after peeking for just one byte, so that the input `echo *`
		// followed by a newline does not hang an interactive shell parser until
		// another byte is input.
		_, p2 := p.peekTwo()
		return p2 != ')'
	}
	return false
}

func (p *Parser) peek() byte {
	if int(p.bsp) >= len(p.bs) {
		p.fill()
	}
	if int(p.bsp) >= len(p.bs) {
		return utf8.RuneSelf
	}
	return p.bs[p.bsp]
}

func (p *Parser) peekTwo() (byte, byte) {
	// TODO: This should loop for slow readers, e.g. those providing one byte at
	// a time. Use a loop and test it with [testing/iotest.OneByteReader].
	if int(p.bsp+1) >= len(p.bs) {
		p.fill()
	}
	if int(p.bsp) >= len(p.bs) {
		return utf8.RuneSelf, utf8.RuneSelf
	}
	if int(p.bsp+1) >= len(p.bs) {
		return p.bs[p.bsp], utf8.RuneSelf
	}
	return p.bs[p.bsp], p.bs[p.bsp+1]
}

func (p *Parser) regToken(r rune) token {
	switch r {
	case '\'':
		p.rune()
		return sglQuote
	case '"':
		p.rune()
		return dblQuote
	case '`':
		// Don't call p.rune, as we need to work out p.openBquotes to
		// properly handle backslashes in the lexer.
		return bckQuote
	case '&':
		switch p.rune() {
		case '&':
			p.rune()
			return andAnd
		case '>':
			switch p.rune() {
			case '|':
				p.rune()
				return rdrAllClob
			case '!':
				if p.lang.in(LangZsh) {
					p.rune()
					return rdrAllClob
				}
			case '>':
				switch p.rune() {
				case '|':
					p.rune()
					return appAllClob
				case '!':
					if p.lang.in(LangZsh) {
						p.rune()
						return appAllClob
					}
				}
				return appAll
			}
			return rdrAll
		case '|':
			if p.lang.in(LangZsh) {
				p.rune()
				return andPipe
			}
		case '!':
			if p.lang.in(LangZsh) {
				p.rune()
				return andBang
			}
		}
		return and
	case '|':
		switch p.rune() {
		case '|':
			p.rune()
			return orOr
		case '&':
			if !p.lang.in(langBashLike | LangMirBSDKorn | LangZsh) {
				break
			}
			p.rune()
			return orAnd
		}
		return or
	case '$':
		switch p.rune() {
		case '\'':
			if !p.lang.in(langBashLike | LangMirBSDKorn | LangZsh) {
				break
			}
			p.rune()
			return dollSglQuote
		case '"':
			if !p.lang.in(langBashLike | LangMirBSDKorn) {
				break
			}
			p.rune()
			return dollDblQuote
		case '{':
			p.rune()
			return dollBrace
		case '[':
			if !p.lang.in(langBashLike) {
				// latter to not tokenise ${$[@]} as $[
				break
			}
			p.rune()
			return dollBrack
		case '(':
			if p.rune() == '(' {
				p.rune()
				return dollDblParen
			}
			return dollParen
		}
		return dollar
	case '(':
		if p.rune() == '(' && p.lang.in(langBashLike|LangMirBSDKorn|LangZsh) && p.quote != testExpr {
			p.rune()
			return dblLeftParen
		}
		return leftParen
	case ')':
		p.rune()
		return rightParen
	case ';':
		switch p.rune() {
		case ';':
			if p.rune() == '&' && p.lang.in(langBashLike) {
				p.rune()
				return dblSemiAnd
			}
			return dblSemicolon
		case '&':
			if !p.lang.in(langBashLike | LangMirBSDKorn | LangZsh) {
				break
			}
			p.rune()
			return semiAnd
		case '|':
			if !p.lang.in(LangMirBSDKorn) {
				break
			}
			p.rune()
			return semiOr
		}
		return semicolon
	case '<':
		switch p.rune() {
		case '<':
			switch p.rune() {
			case '-':
				p.rune()
				return dashHdoc
			case '<':
				p.rune()
				return wordHdoc
			}
			return hdoc
		case '>':
			p.rune()
			return rdrInOut
		case '&':
			p.rune()
			return dplIn
		case '(':
			if !p.lang.in(langBashLike | LangZsh) {
				break
			}
			p.rune()
			return cmdIn
		}
		return rdrIn
	case '>':
		switch p.rune() {
		case '>':
			switch p.rune() {
			case '|':
				p.rune()
				return appClob
			case '!':
				if p.lang.in(LangZsh) {
					p.rune()
					return appClob
				}
			case '&':
				if !p.lang.in(LangZsh) {
					break
				}
				switch p.rune() {
				case '|':
					p.rune()
					return appAllClob // >>&| is an alias for &>>|
				case '!':
					p.rune()
					return appAllClob // >>&! is an alias for &>>|
				}
				return appAll // >>& is an alias for &>>
			}
			return appOut
		case '&':
			r = p.rune()
			if p.lang.in(LangZsh) {
				switch r {
				case '|':
					p.rune()
					return rdrAllClob // >&| is an alias for &>|
				case '!':
					p.rune()
					return rdrAllClob // >&! is an alias for &>|
				}
			}
			return dplOut
		case '|':
			p.rune()
			return rdrClob
		case '!':
			if p.lang.in(LangZsh) {
				p.rune()
				return rdrClob
			}
		case '(':
			if !p.lang.in(langBashLike | LangZsh) {
				break
			}
			p.rune()
			return cmdOut
		}
		return rdrOut
	}
	panic("unreachable")
}

func (p *Parser) dqToken(r rune) token {
	switch r {
	case '"':
		p.rune()
		return dblQuote
	case '`':
		// Don't call p.rune, as we need to work out p.openBquotes to
		// properly handle backslashes in the lexer.
		return bckQuote
	case '$':
		switch p.rune() {
		case '{':
			p.rune()
			return dollBrace
		case '[':
			if !p.lang.in(langBashLike) {
				break
			}
			p.rune()
			return dollBrack
		case '(':
			if p.rune() == '(' {
				p.rune()
				return dollDblParen
			}
			return dollParen
		}
		return dollar
	}
	panic("unreachable")
}

func (p *Parser) paramToken(r rune) token {
	switch r {
	case '}':
		p.rune()
		return rightBrace
	case ':':
		switch p.rune() {
		case '+':
			p.rune()
			return colPlus
		case '-':
			p.rune()
			return colMinus
		case '?':
			p.rune()
			return colQuest
		case '=':
			p.rune()
			return colAssgn
		case '#':
			p.rune()
			return colHash
		case '|':
			p.rune()
			return colPipe
		case '*':
			p.rune()
			return colStar
		}
		return colon
	case '+':
		p.rune()
		return plus
	case '-':
		p.rune()
		return minus
	case '?':
		p.rune()
		return quest
	case '=':
		p.rune()
		return assgn
	case '%':
		if p.rune() == '%' {
			p.rune()
			return dblPerc
		}
		return perc
	case '#':
		if p.rune() == '#' {
			p.rune()
			return dblHash
		}
		return hash
	case '!':
		p.rune()
		return exclMark
	case ']':
		p.rune()
		return rightBrack
	case '/':
		if p.rune() == '/' {
			p.rune()
			return dblSlash
		}
		return slash
	case '^':
		if p.rune() == '^' {
			p.rune()
			return dblCaret
		}
		return caret
	case ',':
		if p.rune() == ',' {
			p.rune()
			return dblComma
		}
		return comma
	case '@':
		p.rune()
		return at
	case '*':
		p.rune()
		return star

	// This func gets called by the parser in [runeByRune] mode;
	// we need to handle EOF and unexpected runes.
	case utf8.RuneSelf:
		return _EOF
	default:
		return illegalTok
	}
}

func (p *Parser) arithmToken(r rune) token {
	switch r {
	case '!':
		if p.rune() == '=' {
			p.rune()
			return nequal
		}
		return exclMark
	case '=':
		if p.rune() == '=' {
			p.rune()
			return equal
		}
		return assgn
	case '~':
		p.rune()
		return tilde
	case '(':
		p.rune()
		return leftParen
	case ')':
		p.rune()
		return rightParen
	case '&':
		switch p.rune() {
		case '&':
			if p.rune() == '=' && p.lang.in(LangZsh) {
				p.rune()
				return andBoolAssgn
			}
			return andAnd
		case '=':
			p.rune()
			return andAssgn
		}
		return and
	case '|':
		switch p.rune() {
		case '|':
			if p.rune() == '=' && p.lang.in(LangZsh) {
				p.rune()
				return orBoolAssgn
			}
			return orOr
		case '=':
			p.rune()
			return orAssgn
		}
		return or
	case '<':
		switch p.rune() {
		case '<':
			if p.rune() == '=' {
				p.rune()
				return shlAssgn
			}
			return hdoc
		case '=':
			p.rune()
			return lequal
		}
		return rdrIn
	case '>':
		switch p.rune() {
		case '>':
			if p.rune() == '=' {
				p.rune()
				return shrAssgn
			}
			return appOut
		case '=':
			p.rune()
			return gequal
		}
		return rdrOut
	case '+':
		switch p.rune() {
		case '+':
			p.rune()
			return addAdd
		case '=':
			p.rune()
			return addAssgn
		}
		return plus
	case '-':
		switch p.rune() {
		case '-':
			p.rune()
			return subSub
		case '=':
			p.rune()
			return subAssgn
		}
		return minus
	case '%':
		if p.rune() == '=' {
			p.rune()
			return remAssgn
		}
		return perc
	case '*':
		switch p.rune() {
		case '*':
			if p.rune() == '=' && p.lang.in(LangZsh) {
				p.rune()
				return powAssgn
			}
			return power
		case '=':
			p.rune()
			return mulAssgn
		}
		return star
	case '/':
		if p.rune() == '=' {
			p.rune()
			return quoAssgn
		}
		return slash
	case '^':
		switch p.rune() {
		case '^':
			if p.rune() == '=' && p.lang.in(LangZsh) {
				p.rune()
				return xorBoolAssgn
			}
			return dblCaret
		case '=':
			p.rune()
			return xorAssgn
		}
		return caret
	case '[':
		p.rune()
		return leftBrack
	case ']':
		p.rune()
		return rightBrack
	case ',':
		p.rune()
		return comma
	case '?':
		p.rune()
		return quest
	case ':':
		p.rune()
		return colon
	case '#':
		p.rune()
		return hash
	case '.':
		p.rune()
		return period
	}
	panic("unreachable")
}

func (p *Parser) newLit(r rune) {
	switch {
	case r < utf8.RuneSelf:
		p.litBs = p.litBuf[:1]
		p.litBs[0] = byte(r)
	case r > escNewl:
		w := utf8.RuneLen(r)
		p.litBs = append(p.litBuf[:0], p.bs[p.bsp-uint(w):p.bsp]...)
	default:
		// don't let r == utf8.RuneSelf go to the second case as [utf8.RuneLen]
		// would return -1
		p.litBs = p.litBuf[:0]
	}
}

func (p *Parser) endLit() (s string) {
	if p.r == utf8.RuneSelf || p.r == escNewl {
		s = string(p.litBs)
	} else {
		s = string(p.litBs[:len(p.litBs)-p.w])
	}
	p.litBs = nil
	return s
}

func (p *Parser) isLitRedir() bool {
	lit := p.litBs[:len(p.litBs)-1]
	if lit[0] == '{' && lit[len(lit)-1] == '}' {
		return ValidName(string(lit[1 : len(lit)-1]))
	}
	return numberLiteral(lit)
}

func positionalRuneParam[T rune | byte](r T) bool {
	switch r {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func singleRuneParam[T rune | byte](r T) bool {
	switch r {
	case '@', '*', '#', '$', '?', '!', '-':
		return true
	}
	return positionalRuneParam(r)
}

func paramNameRune[T rune | byte](r T) bool {
	return asciiLetter(r) || asciiDigit(r) || r == '_'
}

func (p *Parser) advanceLitOther(r rune) {
	tok := _LitWord
loop:
	for p.newLit(r); r != utf8.RuneSelf; r = p.rune() {
		switch r {
		case '\\': // escaped byte follows
			p.rune()
		case '\'', '"', '`', '$':
			tok = _Lit
			break loop
		case '}':
			if p.quote&allParamExp != 0 {
				break loop
			}
		case '/':
			if p.quote != paramExpExp {
				break loop
			}
		case ':', '=', '%', '^', ',', '?', '!', '~', '*':
			if p.quote&allArithmExpr != 0 {
				break loop
			}
		case '.':
			if !p.lang.in(LangZsh) && p.quote&allArithmExpr != 0 {
				break loop
			}
		case '[', ']':
			if p.lang.in(langBashLike|LangMirBSDKorn|LangZsh) && p.quote&allArithmExpr != 0 {
				break loop
			}
			fallthrough
		case '+', '-', ' ', '\t', ';', '&', '>', '<', '|', '(', ')', '\n', '\r':
			if p.quote&allKeepSpaces == 0 {
				break loop
			}
		}
	}
	p.tok, p.val = tok, p.endLit()
}

// zshNumRange peeks at the bytes after '<' to check for a zsh numeric
// range glob pattern like <->, <5->, <-10>, or <5-10>.
func (p *Parser) zshNumRange() bool {
	// Peeking a handful of bytes here should be enough.
	// TODO: This should loop for slow readers, e.g. those providing one byte at
	// a time. Use a loop and test it with [testing/iotest.OneByteReader].
	if int(p.bsp) >= len(p.bs) {
		p.fill()
	}
	rest := p.bs[p.bsp:]
	for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '-' {
		return false
	}
	rest = rest[1:]
	for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		rest = rest[1:]
	}
	return len(rest) > 0 && rest[0] == '>'
}

func (p *Parser) advanceLitNone(r rune) {
	p.eqlOffs = -1
	tok := _LitWord
loop:
	for p.newLit(r); r != utf8.RuneSelf; r = p.rune() {
		switch r {
		case ' ', '\t', '\n', '\r', '&', '|', ';', ')':
			break loop
		case '(':
			break loop
		case '\\': // escaped byte follows
			p.rune()
		case '>', '<':
			if r == '<' && p.lang.in(LangZsh) && p.zshNumRange() {
				// Zsh numeric range glob like <-> or <1-100>; consume until '>'.
				for {
					if r = p.rune(); r == '>' || r == utf8.RuneSelf {
						break
					}
				}
				continue
			}
			if p.peek() == '(' {
				tok = _Lit
			} else if p.isLitRedir() {
				tok = _LitRedir
			}
			break loop
		case '`':
			if p.quote != subCmdBckquo {
				tok = _Lit
			}
			break loop
		case '"', '\'', '$':
			tok = _Lit
			break loop
		case '?', '*', '+', '@', '!':
			if p.extendedGlob() {
				tok = _Lit
				break loop
			}
		case '=':
			if p.eqlOffs < 0 {
				p.eqlOffs = len(p.litBs) - 1
			}
		case '[':
			if p.lang.in(langBashLike|LangMirBSDKorn|LangZsh) && len(p.litBs) > 1 && p.litBs[0] != '[' {
				tok = _Lit
				break loop
			}
		}
	}
	p.tok, p.val = tok, p.endLit()
}

func (p *Parser) advanceLitDquote(r rune) {
	tok := _LitWord
loop:
	for p.newLit(r); r != utf8.RuneSelf; r = p.rune() {
		switch r {
		case '"':
			break loop
		case '\\': // escaped byte follows
			p.rune()
		case escNewl, '`', '$':
			tok = _Lit
			break loop
		}
	}
	p.tok, p.val = tok, p.endLit()
}

func (p *Parser) advanceLitHdoc(r rune) {
	// Unlike the rest of nextKeepSpaces quote states, we handle escaped
	// newlines here. If lastTok==_Lit, then we know we're following an
	// escaped newline, so the first line can't end the heredoc.
	lastTok := p.tok
	for r == escNewl {
		r = p.rune()
		lastTok = _Lit
	}
	p.pos = p.nextPos()

	p.tok = _Lit
	p.newLit(r)
	for p.quote == hdocBodyTabs && r == '\t' {
		r = p.rune()
	}
	lStart := len(p.litBs) - 1
	stop := p.hdocStops[len(p.hdocStops)-1]
	for ; ; r = p.rune() {
		switch r {
		case escNewl, '$':
			p.val = p.endLit()
			return
		case '\\': // escaped byte follows
			p.rune()
		case '`':
			if !p.backquoteEnd() {
				p.val = p.endLit()
				return
			}
			fallthrough
		case '\n', utf8.RuneSelf:
			if p.parsingDoc {
				if r == utf8.RuneSelf {
					p.tok = _LitWord
					p.val = p.endLit()
					return
				}
			} else if lStart == 0 && lastTok == _Lit {
				// This line starts right after an escaped
				// newline, so it should never end the heredoc.
			} else if lStart >= 0 {
				// Compare the current line with the stop word.
				line := p.litBs[lStart:]
				if r != utf8.RuneSelf && len(line) > 0 {
					line = line[:len(line)-1] // minus trailing character
				}
				if bytes.Equal(line, stop) {
					p.tok = _LitWord
					p.val = p.endLit()[:lStart]
					if p.val == "" {
						p.tok = _Newl
					}
					p.hdocStops[len(p.hdocStops)-1] = nil
					return
				}
			}
			if r != '\n' {
				return // hit an unexpected EOF or closing backquote
			}
			for p.quote == hdocBodyTabs && p.peek() == '\t' {
				p.rune()
			}
			lStart = len(p.litBs)
		}
	}
}

func (p *Parser) quotedHdocWord() *Word {
	r := p.r
	p.newLit(r)
	pos := p.nextPos()
	stop := p.hdocStops[len(p.hdocStops)-1]
	for ; ; r = p.rune() {
		if r == utf8.RuneSelf {
			return nil
		}
		for p.quote == hdocBodyTabs && r == '\t' {
			r = p.rune()
		}
		lStart := len(p.litBs) - 1
	runeLoop:
		for {
			switch r {
			case utf8.RuneSelf, '\n':
				break runeLoop
			case '`':
				if p.backquoteEnd() {
					break runeLoop
				}
			case escNewl:
				p.litBs = append(p.litBs, '\\', '\n')
				break runeLoop
			}
			r = p.rune()
		}
		if lStart < 0 {
			continue
		}
		// Compare the current line with the stop word.
		line := p.litBs[lStart:]
		if r != utf8.RuneSelf && len(line) > 0 {
			line = line[:len(line)-1] // minus \n
		}
		if bytes.Equal(line, stop) {
			p.hdocStops[len(p.hdocStops)-1] = nil
			val := p.endLit()[:lStart]
			if val == "" {
				return nil
			}
			return p.wordOne(p.lit(pos, val))
		}
	}
}

func (p *Parser) advanceLitRe(r rune) {
	for p.newLit(r); ; r = p.rune() {
		switch r {
		case '\\':
			p.rune()
		case '(':
			p.rxOpenParens++
		case ')':
			if p.rxOpenParens--; p.rxOpenParens < 0 {
				p.tok, p.val = _LitWord, p.endLit()
				p.quote = testExpr
				return
			}
		case ' ', '\t', '\r', '\n', ';', '&', '>', '<':
			if p.rxOpenParens <= 0 {
				p.tok, p.val = _LitWord, p.endLit()
				p.quote = testExpr
				return
			}
		case '"', '\'', '$', '`':
			p.tok, p.val = _Lit, p.endLit()
			return
		case utf8.RuneSelf:
			p.tok, p.val = _LitWord, p.endLit()
			p.quote = noState
			return
		}
	}
}

func testUnaryOp(val string) UnTestOperator {
	switch val {
	case "!":
		return TsNot
	case "-e", "-a":
		return TsExists
	case "-f":
		return TsRegFile
	case "-d":
		return TsDirect
	case "-c":
		return TsCharSp
	case "-b":
		return TsBlckSp
	case "-p":
		return TsNmPipe
	case "-S":
		return TsSocket
	case "-L", "-h":
		return TsSmbLink
	case "-k":
		return TsSticky
	case "-g":
		return TsGIDSet
	case "-u":
		return TsUIDSet
	case "-G":
		return TsGrpOwn
	case "-O":
		return TsUsrOwn
	case "-N":
		return TsModif
	case "-r":
		return TsRead
	case "-w":
		return TsWrite
	case "-x":
		return TsExec
	case "-s":
		return TsNoEmpty
	case "-t":
		return TsFdTerm
	case "-z":
		return TsEmpStr
	case "-n":
		return TsNempStr
	case "-o":
		return TsOptSet
	case "-v":
		return TsVarSet
	case "-R":
		return TsRefVar
	default:
		return 0
	}
}

func testBinaryOp(val string) BinTestOperator {
	switch val {
	case "=":
		return TsMatchShort
	case "==":
		return TsMatch
	case "!=":
		return TsNoMatch
	case "=~":
		return TsReMatch
	case "-nt":
		return TsNewer
	case "-ot":
		return TsOlder
	case "-ef":
		return TsDevIno
	case "-eq":
		return TsEql
	case "-ne":
		return TsNeq
	case "-le":
		return TsLeq
	case "-ge":
		return TsGeq
	case "-lt":
		return TsLss
	case "-gt":
		return TsGtr
	default:
		return 0
	}
}
