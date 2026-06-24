package agent

import (
	"regexp"
	"strings"
)

var reTransientUserBlock = regexp.MustCompile(`(?s)^\s*<(?:reasoning-language|memory-update|background-jobs)>.*?</(?:reasoning-language|memory-update|background-jobs)>\s*\n?`)

// StripTransientUserBlocks removes controller-injected transient XML blocks
// from persisted user messages before deriving display text, previews, or
// titles. The blocks are sent in user turns so they never affect the stable
// prompt prefix, but they should not become user-facing text later.
func StripTransientUserBlocks(content string) string {
	s := content
	for {
		next := reTransientUserBlock.ReplaceAllStringFunc(s, func(string) string {
			return ""
		})
		if next == s {
			break
		}
		s = next
	}
	return strings.TrimLeft(s, " \t\r\n")
}

// StripInjectedContext removes model-facing context blocks prepended to a user
// turn (for example the resolved contents of @file references) and returns the
// user-authored prompt that followed them. The injected block is persisted as part
// of the user message so the model can see it, but previews/titles should be
// based on what the user actually typed.
func StripInjectedContext(content string) string {
	s := strings.TrimLeft(content, " \t\r\n")
	const marker = "Referenced context:"
	if !strings.HasPrefix(s, marker) {
		return content
	}
	rest := strings.TrimLeft(s[len(marker):], " \t\r\n")
	consumed := false
	for {
		rest = strings.TrimLeft(rest, " \t\r\n")
		if !strings.HasPrefix(rest, "<") {
			break
		}
		endOpen := strings.IndexByte(rest, '>')
		if endOpen < 0 {
			break
		}
		tag := rest[1:endOpen]
		if i := strings.IndexAny(tag, " \t\r\n"); i >= 0 {
			tag = tag[:i]
		}
		if tag == "" || strings.HasPrefix(tag, "/") {
			break
		}
		closeTag := "</" + tag + ">"
		endClose := strings.Index(rest[endOpen+1:], closeTag)
		if endClose < 0 {
			break
		}
		rest = rest[endOpen+1+endClose+len(closeTag):]
		consumed = true
	}
	if !consumed {
		return content
	}
	rest = strings.TrimLeft(rest, " \t\r\n")
	if strings.HasPrefix(rest, marker) {
		return StripInjectedContext(rest)
	}
	return strings.TrimSpace(rest)
}

// UserPreviewText returns the user-authored part of a persisted user message.
func UserPreviewText(content string) string {
	s := StripTransientUserBlocks(content)
	s = StripInjectedContext(s)
	s = HandoffTask(s)
	s = StripTransientUserBlocks(s)
	s = StripInjectedContext(s)
	return strings.TrimSpace(s)
}
