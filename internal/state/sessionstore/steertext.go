package sessionstore

import "strings"

// MidTurnSteerPrefix marks user messages that were injected mid-turn as
// guidance (via Steer). The model sees them as instructions; frontends
// display them as a notice, not a regular user bubble.
const MidTurnSteerPrefix = "[Mid-turn steer queued by the user. Do not treat this as a new task; use it only as additional guidance for the current task after completing the current step.]"

// HostNoticePrefix marks guidance the host runtime queued for itself, such as
// recovery advice after a tool failed. It rides the steer path but must not
// claim the user spoke: a model told the user interrupted starts answering a
// person who said nothing, and cannot tell a real interruption from this one.
const HostNoticePrefix = "[Mid-turn notice from the host runtime; the user did not send this. Do not treat it as a new task or as a message to answer; use it only as additional guidance for the current task after completing the current step.]"

func MidTurnSteerMessage(text string, host bool) string {
	if host {
		return HostNoticePrefix + "\n" + text
	}
	return MidTurnSteerPrefix + "\n" + text
}

// SteerText reports whether content is a mid-turn steer and returns the user
// text with the wrapper stripped — the prefix and its separator only, never
// spaces, so replay matches the live Steer rendering character-for-character.
// The transient language blocks withTurnPreferences prepends and the delivery
// marker it appends are framing: both are skipped rather than returned.
func SteerText(content string) (string, bool) {
	text, _, ok := SteerKind(content)
	return text, ok
}

// SteerKind is SteerText plus whose steer it was. Only the prefix tells the
// two apart, so a caller that shows the line to the person asks here instead
// of reading the wording: rendering the host's mid-turn notice as something
// they said puts words in their mouth in the one place they read them back.
func SteerKind(content string) (string, bool, bool) {
	s := content
	for {
		for i, prefix := range []string{MidTurnSteerPrefix, HostNoticePrefix} {
			after, found := strings.CutPrefix(s, prefix)
			if !found {
				continue
			}
			// Strip only the "\n" separator, preserving the user's original text.
			after = strings.TrimPrefix(after, "\n")
			if trimmed, cut := strings.CutSuffix(after, "\n\n"+DeliveryRuntimeMarker); cut {
				after = trimmed
			}
			return after, i == 1, true
		}
		next, ok := trimLeadingSteerWrapper(s)
		if !ok {
			return "", false, false
		}
		s = next
	}
}

// trimLeadingSteerWrapper removes one leading transient block the host may
// have placed ahead of the steer prefix. It walks TransientUserBlockTags for
// the same reason hasLeadingInjectedBlock does: a tag the injector knows and
// this walk does not stops it early, and the steer behind that block reads
// back as the user having typed the host's own instructions at them.
func trimLeadingSteerWrapper(content string) (string, bool) {
	s := strings.TrimLeft(content, " \t\r\n")
	for _, tag := range TransientUserBlockTags {
		if !HasOpenTag(s, tag) {
			continue
		}
		if rest, ok := TrimLeadingTransientBlock(s, tag); ok {
			return rest, true
		}
	}
	return content, false
}

// HasOpenTag reports whether s opens with tag, with or without attributes
// (hook-context and capability-route carry them).
func HasOpenTag(s, tag string) bool {
	return strings.HasPrefix(s, "<"+tag+">") || strings.HasPrefix(s, "<"+tag+" ")
}

func TrimLeadingTransientBlock(content, tag string) (string, bool) {
	closeTag := "</" + tag + ">"
	_, after, ok := strings.Cut(content, closeTag)
	if !ok {
		return content, false
	}
	return strings.TrimLeft(after, " \t\r\n"), true
}
