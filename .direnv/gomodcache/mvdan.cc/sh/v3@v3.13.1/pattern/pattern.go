// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package pattern allows working with shell pattern matching notation, also
// known as wildcards or globbing.
//
// For reference, see
// https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_13.
package pattern

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Mode can be used to supply a number of options to the package's functions.
// Not all functions change their behavior with all of the options below.
type Mode uint

type SyntaxError struct {
	msg string
	err error
}

func (e SyntaxError) Error() string { return e.msg }

func (e SyntaxError) Unwrap() error { return e.err }

// NegExtGlobGroup represents the byte offset range of a single !(expr) group
// within a pattern string. Start is the offset of '!', End is one past ')'.
type NegExtGlobGroup struct {
	Start, End int
}

// NegExtGlobError is returned by [Regexp] when an extglob negation operator
// !(pattern-list) is encountered, as Go's [regexp] package does not support
// negative lookahead. Callers can handle this by negating the result of
// matching the inner pattern.
type NegExtGlobError struct {
	Groups []NegExtGlobGroup
}

func (e *NegExtGlobError) Error() string {
	return "extglob !(...) is not supported in this scenario"
}

// TODO(v4): flip NoGlobStar to be opt-in via GlobStar, matching bash
// TODO(v4): flip EntireString to be opt-out via PartialMatch, as EntireString causes subtle bugs when forgotten
// TODO(v4): rename NoGlobCase to CaseInsensitive for readability

const (
	Shortest          Mode = 1 << iota // prefer the shortest match.
	Filenames                          // "*" and "?" don't match slashes; only "**" does; only makes sense with EntireString too
	EntireString                       // match the entire string using ^$ delimiters
	NoGlobCase                         // do case-insensitive match (that is, use (?i) in the regexp); shopt "nocaseglob"
	NoGlobStar                         // do not support "**"; negated shopt "globstar"
	GlobLeadingDot                     // let wildcards match leading dots in filenames; shopt "dotglob"
	ExtendedOperators                  // support extended pattern matching operators; shopt "extglob" for pathname expansion
)

// Regexp turns a shell pattern into a regular expression that can be used with
// [regexp.Compile]. It will return an error if the input pattern was incorrect.
// Otherwise, the returned expression can be passed to [regexp.MustCompile].
//
// For example, Regexp(`foo*bar?`, true) returns `foo.*bar.`.
//
// Note that this function (and [QuoteMeta]) should not be directly used with file
// paths if Windows is supported, as the path separator on that platform is the
// same character as the escaping character for shell patterns.
func Regexp(pat string, mode Mode) (string, error) {
	// If there are no special pattern matching or regular expression characters,
	// and we don't need to insert extras for the modes affecting non-special characters,
	// we can directly return the input string as a short-cut.
	if mode&(EntireString|NoGlobCase) == 0 {
		needsEscaping := false
	noopLoop:
		for _, r := range pat {
			switch r {
			// including those that need escaping since they are
			// regular expression metacharacters
			case '*', '?', '[', '\\', '.', '+', '(', ')', '|',
				']', '{', '}', '^', '$':
				needsEscaping = true
				break noopLoop
			}
		}
		if !needsEscaping {
			return pat, nil
		}
	}
	var sb strings.Builder
	// Enable matching `\n` with the `.` metacharacter as globs match `\n`
	sb.WriteString(`(?s`)
	if mode&NoGlobCase != 0 {
		sb.WriteString(`i`)
	}
	if mode&Shortest != 0 {
		sb.WriteString(`U`)
	}
	sb.WriteString(`)`)
	if mode&EntireString != 0 {
		sb.WriteString(`^`)
	}
	sl := stringLexer{s: pat}
	var negGroups []NegExtGlobGroup
	for {
		if err := regexpNext(&sb, &sl, mode); err == io.EOF {
			break
		} else if err != nil {
			negErr, ok := err.(*NegExtGlobError)
			if !ok {
				return "", err
			}
			negGroups = append(negGroups, negErr.Groups...)
		}
	}
	if len(negGroups) > 0 {
		return "", &NegExtGlobError{Groups: negGroups}
	}
	if mode&EntireString != 0 {
		sb.WriteString(`$`)
	}
	return sb.String(), nil
}

// stringLexer helps us tokenize a pattern string.
// Note that we can use the null byte '\x00' to signal "no character" as shell strings cannot contain null bytes.
type stringLexer struct {
	s string
	i int
}

func (sl *stringLexer) next() rune {
	if sl.i >= len(sl.s) {
		return '\x00'
	}
	c, size := utf8.DecodeRuneInString(sl.s[sl.i:])
	sl.i += size
	return c
}

func (sl *stringLexer) last() rune {
	if sl.i < 2 {
		return '\x00'
	}
	c, _ := utf8.DecodeLastRuneInString(sl.s[:sl.i-1])
	return c
}

func (sl *stringLexer) peekNext() rune {
	if sl.i >= len(sl.s) {
		return '\x00'
	}
	c, _ := utf8.DecodeRuneInString(sl.s[sl.i:])
	return c
}

func (sl *stringLexer) peekRest() string {
	return sl.s[sl.i:]
}

func regexpNext(sb *strings.Builder, sl *stringLexer, mode Mode) error {
	c := sl.next()
	if mode&ExtendedOperators != 0 {
		// Handle extended pattern matching operators separately,
		// given that they can be one of many two-character prefixes.
		// Note that we recurse into the same function in a loop,
		// as each of the patterns in the list separated by '|' is a regular pattern.
		switch op := c; op {
		case '!', '?', '*', '+', '@':
			if sl.peekNext() != '(' {
				break
			}
			start := sl.i - 1       // position of the operator
			sb.WriteRune(sl.next()) // (
		nestedLoop:
			for {
				switch sl.peekNext() {
				case ')':
					break nestedLoop
				case '|':
					// extended operators support a list of "or" separated expressions
					sb.WriteRune(sl.next())
					continue
				}
				if err := regexpNext(sb, sl, mode); err == io.EOF {
					break
				} else if err != nil {
					return err
				}
			}
			sb.WriteRune(sl.next()) // )
			if op == '!' {
				return &NegExtGlobError{Groups: []NegExtGlobGroup{{Start: start, End: sl.i}}}
			}
			if op != '@' {
				// @( is [syntax.GlobOne] for matching once; no suffix needed
				sb.WriteRune(op)
			}
			return nil
		}
	}
	switch c {
	case '\x00':
		return io.EOF
	case '*':
		if mode&Filenames == 0 {
			// * - matches anything when not in filename mode
			sb.WriteString(`.*`)
			break
		}
		// "**" only acts as globstar if it is alone as a path element.
		singleBefore := sl.i == 1 || sl.last() == '/'
		if sl.peekNext() == '*' {
			sl.i++
			singleAfter := sl.i == len(sl.s) || sl.peekNext() == '/'
			if mode&NoGlobStar == 0 && singleBefore && singleAfter {
				// ** - match any number of slashes or "*" path elements
				slashSuffix := sl.peekNext() == '/'
				if slashSuffix {
					// **/ - like "**" but requiring a trailing slash when matching
					sl.i++
					// wrap the expression to ensure that any match has a slash suffix
					sb.WriteString(`(`)
				}
				if mode&GlobLeadingDot == 0 {
					sb.WriteString(`(/|[^/.][^/]*)*`)
				} else {
					// with GlobLeadingDot (dotglob), match anything at all
					sb.WriteString(`.*`)
				}
				if slashSuffix {
					sb.WriteString(`/)?`)
				}
				break
			}
			// foo**, **bar, or NoGlobStar - behaves like "*" below
		}
		// * - matches anything except slashes and leading dots
		if singleBefore && mode&GlobLeadingDot == 0 {
			sb.WriteString(`([^/.][^/]*)?`)
		} else {
			// with GlobLeadingDot (dotglob), match anything except slashes
			sb.WriteString(`[^/]*`)
		}
	case '?':
		if mode&Filenames != 0 {
			sb.WriteString(`[^/]`)
		} else {
			sb.WriteByte('.')
		}
	case '\\':
		c = sl.next()
		if c == '\x00' {
			return &SyntaxError{msg: `\ at end of pattern`}
		}
		sb.WriteString(regexp.QuoteMeta(string(c)))
	case '[':
		// TODO: surely char classes can be mixed with others, e.g. [[:foo:]xyz]
		if name, err := charClass(sl.peekRest()); err != nil {
			return &SyntaxError{msg: "charClass invalid", err: err}
		} else if name != "" {
			sb.WriteByte('[')
			sb.WriteString(name)
			sl.i += len(name)
			break
		}
		if mode&Filenames != 0 {
			for i, c := range sl.peekRest() {
				if i > 0 && c == ']' {
					break
				} else if c == '/' {
					sb.WriteString(`\[`)
					return nil
				}
			}
		}
		sb.WriteRune(c)
		if c = sl.next(); c == '\x00' {
			return &SyntaxError{msg: "[ was not matched with a closing ]"}
		}
		switch c {
		case '!', '^':
			sb.WriteByte('^')
			if c = sl.next(); c == '\x00' {
				return &SyntaxError{msg: "[ was not matched with a closing ]"}
			}
		}
		if c == ']' {
			sb.WriteByte(']')
			if c = sl.next(); c == '\x00' {
				return &SyntaxError{msg: "[ was not matched with a closing ]"}
			}
		}
		for {
			sb.WriteRune(c)
			switch c {
			case '\x00':
				return &SyntaxError{msg: "[ was not matched with a closing ]"}
			case '\\':
				if c = sl.next(); c != '0' {
					sb.WriteRune(c)
				}
			case '-':
				start := sl.last()
				end := sl.peekNext()
				// TODO: what about overlapping ranges, like: [a--z]
				if end != ']' && start > end {
					return &SyntaxError{msg: fmt.Sprintf("invalid range: %c-%c", start, end)}
				}
			case ']':
				return nil
			}
			c = sl.next()
		}
	default:
		if c > utf8.RuneSelf {
			sb.WriteRune(c)
		} else {
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return nil
}

func charClass(s string) (string, error) {
	if strings.HasPrefix(s, "[.") || strings.HasPrefix(s, "[=") {
		return "", fmt.Errorf("collating features not available")
	}
	name, ok := strings.CutPrefix(s, "[:")
	if !ok {
		return "", nil
	}
	name, _, ok = strings.Cut(name, ":]]")
	if !ok {
		return "", fmt.Errorf("[[: was not matched with a closing :]]")
	}
	switch name {
	case "alnum", "alpha", "ascii", "blank", "cntrl", "digit", "graph",
		"lower", "print", "punct", "space", "upper", "word", "xdigit":
	default:
		return "", fmt.Errorf("invalid character class: %q", name)
	}
	return s[:len(name)+5], nil
}

// HasMeta returns whether a string contains any unescaped pattern
// metacharacters: '*', '?', or '['. When the function returns false, the given
// pattern can only match at most one string.
//
// For example, HasMeta(`foo\*bar`) returns false, but HasMeta(`foo*bar`)
// returns true.
//
// This can be useful to avoid extra work, like [Regexp]. Note that this
// function cannot be used to avoid [QuoteMeta], as backslashes are quoted by
// that function but ignored here.
//
// The [Mode] parameter is unused, and will be removed in v4.
func HasMeta(pat string, mode Mode) bool {
	for i := 0; i < len(pat); i++ {
		switch pat[i] {
		case '\\':
			i++
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// QuoteMeta returns a string that quotes all pattern metacharacters in the
// given text. The returned string is a pattern that matches the literal text.
//
// For example, QuoteMeta(`foo*bar?`) returns `foo\*bar\?`.
//
// The [Mode] parameter is unused, and will be removed in v4.
func QuoteMeta(pat string, mode Mode) string {
	needsEscaping := false
loop:
	for _, r := range pat {
		switch r {
		case '*', '?', '[', '\\':
			needsEscaping = true
			break loop
		}
	}
	if !needsEscaping { // short-cut without a string copy
		return pat
	}
	var sb strings.Builder
	for _, r := range pat {
		switch r {
		case '*', '?', '[', '\\':
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
