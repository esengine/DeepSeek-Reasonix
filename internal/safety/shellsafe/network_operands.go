package shellsafe

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/shellparse"
)

// dynamicPart stands for a word part whose value the host cannot know: a
// parameter, a substitution, arithmetic. It may expand to separators.
const dynamicPart = '\x00'

// maxNetworkVariants bounds the brace alternatives read from one word.
const maxNetworkVariants = 64

// OperandsNameNetworkPath reports whether any word of a command, in any nested
// statement, is or could expand to a Windows network path. A permission verdict
// only; effects are classified elsewhere. It reads each word's lexical original,
// through option and PowerShell provider prefixes, with `/` and `\` both as
// separators; two leading separators and a host are required.
func OperandsNameNetworkPath(command string) bool {
	if !fileutil.HostIsWindows {
		return false
	}
	file, err := shellparse.ParseBash(command)
	if err != nil {
		return true
	}
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if found {
			return false
		}
		if word, ok := node.(*syntax.Word); ok && wordMayBeNetworkPath(word) {
			found = true
		}
		return !found
	})
	return found
}

func wordMayBeNetworkPath(word *syntax.Word) bool {
	for _, skeleton := range wordSkeletons(word.Parts) {
		for _, start := range candidateStarts(skeleton) {
			if slices.ContainsFunc(operandForms(skeleton[start:]), mayBeNetworkOperand) {
				return true
			}
		}
	}
	return false
}

// wordSkeletons renders a word's lexical text with every dynamic part replaced
// by dynamicPart. An oversized word collapses to one dynamic skeleton.
func wordSkeletons(parts []syntax.WordPart) []string {
	out := []string{""}
	for _, part := range parts {
		var piece string
		switch p := part.(type) {
		case *syntax.Lit:
			piece = p.Value
		case *syntax.SglQuoted:
			piece = p.Value
		case *syntax.DblQuoted:
			inner := wordSkeletons(p.Parts)
			next := make([]string, 0, len(out)*len(inner))
			for _, a := range out {
				for _, b := range inner {
					next = append(next, a+b)
				}
			}
			if len(next) > maxNetworkVariants {
				return []string{string(dynamicPart)}
			}
			out = next
			continue
		default:
			piece = string(dynamicPart)
		}
		for i := range out {
			out[i] += piece
		}
	}
	return out
}

// candidateStarts are the offsets a path may begin at: the word's start, and
// after each `,` (a PowerShell array or a brace alternative) or `{`.
func candidateStarts(skeleton string) []int {
	starts := []int{0}
	for i := 1; i < len(skeleton); i++ {
		if skeleton[i-1] == '{' || skeleton[i-1] == ',' {
			starts = append(starts, i)
		}
	}
	return starts
}

// operandForms returns what follows an option name, an `=`, and a PowerShell
// provider prefix, besides the text itself.
func operandForms(text string) []string {
	forms := []string{text}
	if strings.HasPrefix(text, "-") {
		i := 0
		for i < len(text) && text[i] == '-' {
			i++
		}
		for i < len(text) && (text[i] == '_' || text[i] == '-' || text[i] >= '0' && text[i] <= '9' || text[i]|0x20 >= 'a' && text[i]|0x20 <= 'z') {
			i++
		}
		if i < len(text) && (text[i] == '=' || text[i] == ':') {
			i++
		}
		forms = append(forms, text[i:])
	}
	if _, after, ok := strings.Cut(text, "="); ok {
		forms = append(forms, after)
	}
	for _, form := range forms {
		if rest, ok := withoutProviderPrefix(form); ok {
			forms = append(forms, rest)
		}
	}
	return forms
}

func withoutProviderPrefix(text string) (string, bool) {
	rest := text
	if len(rest) >= len(`Microsoft.PowerShell.Core\`) && strings.EqualFold(rest[:len(`Microsoft.PowerShell.Core\`)], `Microsoft.PowerShell.Core\`) {
		rest = rest[len(`Microsoft.PowerShell.Core\`):]
	}
	if len(rest) >= len("FileSystem::") && strings.EqualFold(rest[:len("FileSystem::")], "FileSystem::") {
		return rest[len("FileSystem::"):], true
	}
	return rest, rest != text
}

func isSeparator(c byte) bool { return c == '/' || c == '\\' }

// mayBeNetworkOperand tests the leading run of separators and dynamic parts.
func mayBeNetworkOperand(operand string) bool {
	seps, dynamics, i := 0, 0, 0
	for ; i < len(operand); i++ {
		switch {
		case isSeparator(operand[i]):
			seps++
		case operand[i] == dynamicPart:
			dynamics++
		default:
			goto head
		}
	}
head:
	if dynamics > 0 {
		return seps >= 1
	}
	if seps < 2 || i >= len(operand) || operand[i] == ' ' {
		return false
	}
	return fileutil.IsNetworkPath(strings.NewReplacer("/", `\`).Replace(operand[:i]) + operand[i:])
}
