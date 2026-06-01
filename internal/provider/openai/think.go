package openai

import "strings"

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

type thinkState int

const (
	thinkProbe thinkState = iota
	thinkInside
	thinkPassthrough
)

// thinkSplitter peels a leading <think>...</think> block out of the content
// stream into reasoning text. MiniMax-M3 inlines its chain-of-thought this way
// instead of populating reasoning_content. It only arms on a <think> at the very
// start of the turn, so an answer that merely mentions the tag is never hijacked.
type thinkSplitter struct {
	state thinkState
	buf   string
}

func (t *thinkSplitter) push(s string) (reasoning, text string) {
	switch t.state {
	case thinkPassthrough:
		return "", s
	case thinkInside:
		return t.scanClose(s)
	}

	t.buf += s
	trimmed := strings.TrimLeft(t.buf, " \t\r\n")
	if len(trimmed) < len(thinkOpen) {
		if strings.HasPrefix(thinkOpen, trimmed) {
			return "", "" // still could become <think> once more arrives
		}
		return "", t.drainPassthrough()
	}
	if strings.HasPrefix(trimmed, thinkOpen) {
		t.state = thinkInside
		t.buf = ""
		return t.scanClose(trimmed[len(thinkOpen):])
	}
	return "", t.drainPassthrough()
}

func (t *thinkSplitter) scanClose(s string) (reasoning, text string) {
	t.buf += s
	if idx := strings.Index(t.buf, thinkClose); idx >= 0 {
		r := t.buf[:idx]
		rest := strings.TrimLeft(t.buf[idx+len(thinkClose):], " \t\r\n")
		t.buf = ""
		t.state = thinkPassthrough
		return r, rest
	}
	keep := markerSuffixLen(t.buf, thinkClose)
	r := t.buf[:len(t.buf)-keep]
	t.buf = t.buf[len(t.buf)-keep:]
	return r, ""
}

// flush emits whatever is buffered when the stream ends mid-decision: an
// unterminated <think> block is reasoning; anything else is text.
func (t *thinkSplitter) flush() (reasoning, text string) {
	if t.buf == "" {
		return "", ""
	}
	out := t.buf
	t.buf = ""
	if t.state == thinkInside {
		return out, ""
	}
	return "", out
}

func (t *thinkSplitter) drainPassthrough() string {
	t.state = thinkPassthrough
	out := t.buf
	t.buf = ""
	return out
}

// markerSuffixLen returns the length of the longest proper suffix of s that is a
// prefix of marker — the tail to hold back in case the rest of the tag arrives
// in the next delta.
func markerSuffixLen(s, marker string) int {
	max := len(marker) - 1
	if max > len(s) {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(marker, s[len(s)-n:]) {
			return n
		}
	}
	return 0
}
