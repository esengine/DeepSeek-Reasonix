package shellparse

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

const maxWrapperDepth = 16

// Invocation is a simple command read through its transparent wrappers.
type Invocation struct {
	// Layers is the static argv prefix of the command, then of each command a
	// wrapper hands the call to; a path-spelled program is named by its base.
	Layers [][]string
	// Opaque marks a wrapper whose target cannot be named statically.
	Opaque bool
}

type wrapperSpec struct {
	shortFlag string
	shortArg  string
	longFlag  []string
	longArg   []string
	assigns   bool
	anyEquals bool
	numeric   bool
	reportsOn string
}

// wrapperSpecs is the closed set of programs treated as transparent: each runs
// the command in its arguments unchanged. A program outside it is a different
// program, never a wrapper, so nothing about it is inferred.
var wrapperSpecs = map[string]wrapperSpec{
	"env": {
		shortFlag: "iv", shortArg: "uC", assigns: true, anyEquals: true,
		longFlag: []string{"-", "--ignore-environment", "--debug"},
		longArg:  []string{"--unset", "--chdir"},
	},
	"sudo": {
		shortFlag: "AbBEHknPS", shortArg: "ugphCDRTrtU", assigns: true,
		longFlag: []string{"--askpass", "--background", "--bell", "--preserve-env", "--non-interactive", "--stdin", "--set-home", "--reset-timestamp"},
		longArg:  []string{"--user", "--group", "--host", "--prompt", "--close-from", "--chdir", "--chroot", "--role", "--type", "--other-user", "--command-timeout"},
	},
	"doas":    {shortFlag: "nL", shortArg: "uC"},
	"command": {shortFlag: "p", reportsOn: "vV"},
	"builtin": {},
	"exec":    {shortFlag: "cl", shortArg: "a"},
	"nohup":   {},
	"time": {
		shortFlag: "pvalh", shortArg: "of",
		longFlag: []string{"--portability", "--verbose", "--append"},
		longArg:  []string{"--output", "--format"},
	},
	"nice": {shortArg: "n", longArg: []string{"--adjustment"}, numeric: true},
}

type argWord struct {
	text   string
	static bool
	assign bool
	equals bool
	lead   bool
	dash   bool
}

// PeelWrappers follows the wrappers in wrapperSpecs down to the program a single
// simple command runs. ok is false for compound lists, pipelines, groups and
// parse errors, which stay with their per-segment callers. `time`, `!` and a
// program spelled as a path (base name, `.exe` and case folded) are transparent.
func PeelWrappers(command string) (inv Invocation, ok bool) {
	file, err := ParseBash(command)
	if err != nil || len(file.Stmts) != 1 {
		return inv, false
	}
	stmt := file.Stmts[0]
	for stmt != nil {
		if stmt.Coprocess {
			return inv, false
		}
		if clause, isTime := stmt.Cmd.(*syntax.TimeClause); isTime {
			stmt = clause.Stmt
			continue
		}
		break
	}
	if stmt == nil {
		return inv, false
	}
	call, isCall := stmt.Cmd.(*syntax.CallExpr)
	if !isCall {
		return inv, false
	}
	words := make([]argWord, 0, len(call.Args))
	for _, arg := range call.Args {
		if wordHasUnescapedBrace(arg) {
			syntax.SplitBraces(arg)
		}
		text, static := StaticWord(arg)
		prefix := staticPrefix(arg)
		words = append(words, argWord{
			text: text, static: static,
			assign: strings.Contains(prefix, "=") && IsAssignment(prefix),
			equals: strings.Index(prefix, "=") > 0,
			lead:   strings.HasPrefix(prefix, "="),
			dash:   strings.HasPrefix(prefix, "-"),
		})
	}
	for depth := 0; len(words) > 0; depth++ {
		if depth >= maxWrapperDepth {
			inv.Opaque = true
			return inv, true
		}
		if !words[0].static {
			inv.Opaque = depth > 0
			return inv, true
		}
		layer := make([]string, 0, len(words))
		for _, w := range words {
			if !w.static {
				break
			}
			layer = append(layer, w.text)
		}
		program := programName(layer[0])
		layer[0] = program
		inv.Layers = append(inv.Layers, layer)
		spec, wrapper := wrapperSpecs[strings.ToLower(program)]
		if !wrapper {
			return inv, true
		}
		rest, status := spec.unwrap(words[1:])
		switch status {
		case unwrapNone:
			return inv, true
		case unwrapOpaque:
			inv.Opaque = true
			return inv, true
		}
		words = rest
	}
	return inv, true
}

type unwrapStatus uint8

const (
	unwrapped unwrapStatus = iota
	unwrapNone
	unwrapOpaque
)

func (s wrapperSpec) unwrap(args []argWord) ([]argWord, unwrapStatus) {
	for i := 0; i < len(args); i++ {
		w := args[i]
		if s.assigns && !w.dash {
			if w.lead {
				return nil, unwrapOpaque
			}
			if w.assign || w.equals && s.anyEquals {
				continue
			}
			if w.equals {
				return nil, unwrapOpaque
			}
		}
		if !w.static || !strings.HasPrefix(w.text, "-") || w.text == "-" && !slices.Contains(s.longFlag, "-") {
			return args[i:], unwrapped
		}
		text := w.text
		switch {
		case text == "--":
			if i+1 >= len(args) {
				return nil, unwrapNone
			}
			return args[i+1:], unwrapped
		case strings.HasPrefix(text, "--"):
			name, _, hasValue := strings.Cut(text, "=")
			switch {
			case slices.Contains(s.longFlag, name):
			case slices.Contains(s.longArg, name):
				if !hasValue {
					i++
					if i >= len(args) {
						return nil, unwrapOpaque
					}
				}
			default:
				return nil, unwrapOpaque
			}
		case text == "-":
		case s.numeric && allDigits(text[1:]):
		default:
			consumed, status := s.shortCluster(text[1:], len(args) > i+1)
			switch status {
			case unwrapOpaque:
				return nil, unwrapOpaque
			case unwrapNone:
				return nil, unwrapNone
			}
			i += consumed
		}
	}
	return nil, unwrapNone
}

// shortCluster reads one `-abc` word and returns how many following words it
// consumed as option values.
func (s wrapperSpec) shortCluster(flags string, valueFollows bool) (int, unwrapStatus) {
	for i := range len(flags) {
		c := flags[i]
		switch {
		case strings.IndexByte(s.reportsOn, c) >= 0:
			return 0, unwrapNone
		case strings.IndexByte(s.shortFlag, c) >= 0:
		case strings.IndexByte(s.shortArg, c) >= 0:
			if i+1 < len(flags) {
				return 0, unwrapped
			}
			if !valueFollows {
				return 0, unwrapOpaque
			}
			return 1, unwrapped
		default:
			return 0, unwrapOpaque
		}
	}
	return 0, unwrapped
}

func staticPrefix(word *syntax.Word) string {
	var prefix strings.Builder
	if word == nil {
		return ""
	}
	for _, part := range word.Parts {
		value, ok := staticWordPart(part, false)
		if !ok {
			break
		}
		prefix.WriteString(value)
	}
	return prefix.String()
}

func programName(word string) string {
	original := word
	hasPath := strings.ContainsAny(word, `/\`)
	if hasPath {
		word = word[strings.LastIndexAny(word, `/\`)+1:]
	}
	if len(word) > len(".exe") && strings.EqualFold(word[len(word)-len(".exe"):], ".exe") {
		word = word[:len(word)-len(".exe")]
		hasPath = true
	}
	if word == "" {
		return original
	}
	if hasPath {
		word = strings.ToLower(word)
	}
	return word
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
