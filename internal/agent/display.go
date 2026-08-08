package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Display recovery for user-authored blocks folded by the desktop composer.
// The provider-facing submit form carries transport framing — expanded paste
// blocks (--- Begin / End ---), the <reasonix-selected-chat-context> wrapper,
// and referenced-session preambles — while the bubble shows compact labels.
// These helpers deterministically fold the submit form back into the display
// form so history replay and live steer events render like a regular user
// bubble instead of leaking that framing.

// pastedTextDisplayLabelPattern matches the folded paste label rendered by the
// composer in every supported locale.
var pastedTextDisplayLabelPattern = regexp.MustCompile(`^\[(?:已粘贴文本|已貼上文字|Pasted text) #[0-9]+ · [0-9]+ (?:行|lines)\]$`)

// CollapseLegacyExpandedPasteDisplay repairs user-authored text whose expanded
// pasted-text blocks were persisted verbatim (transcripts written before
// RawContent existed, or mid-turn steer messages). Each
// "--- Begin [label] ---\n…\n--- End [label] ---" block collapses back to its
// compact label; the expanded body remains only in the submit form.
func CollapseLegacyExpandedPasteDisplay(content string) string {
	const beginPrefix = "--- Begin "
	for scan := 0; scan < len(content); {
		beginOffset := strings.Index(content[scan:], beginPrefix)
		if beginOffset < 0 {
			break
		}
		begin := scan + beginOffset
		labelStart := begin + len(beginPrefix)
		labelEndOffset := strings.Index(content[labelStart:], " ---")
		if labelEndOffset < 0 {
			break
		}
		labelEnd := labelStart + labelEndOffset
		label := content[labelStart:labelEnd]
		beginEnd := labelEnd + len(" ---")
		if !pastedTextDisplayLabelPattern.MatchString(label) {
			scan = beginEnd
			continue
		}
		endMarker := "--- End " + label + " ---"
		endOffset := strings.Index(content[beginEnd:], endMarker)
		if endOffset < 0 {
			scan = beginEnd
			continue
		}
		labelCopy := strings.LastIndex(content[:begin], label)
		if labelCopy < 0 || strings.TrimSpace(content[labelCopy+len(label):begin]) != "" {
			scan = beginEnd
			continue
		}
		end := beginEnd + endOffset + len(endMarker)
		content = content[:labelCopy+len(label)] + content[end:]
		scan = labelCopy + len(label)
	}
	return strings.TrimSpace(content)
}

// RecoverSteerDisplay converts a mid-turn steer's provider-facing submit form
// back into the compact user-facing display form. Order matters: the
// referenced-session preamble leads the text, expanded paste blocks sit in the
// body, and the selected-context block is the trailing suffix.
func RecoverSteerDisplay(text string) string {
	s := stripReferencedSessionPreamble(text)
	s = CollapseLegacyExpandedPasteDisplay(s)
	s = collapseSelectedContextBlock(s)
	return strings.TrimSpace(s)
}

// referencedSessionPreamble marks the past:chats transcript the composer
// prepends to submit text. Headers and footers are locale literals generated
// by the frontend; the user's actual words follow the footer.
var referencedSessionPreamble = []struct{ header, footer string }{
	{"The user referenced the following earlier session context:", "Current user request:"},
	{"以下是用户引用的历史会话上下文：", "当前用户问题："},
	{"以下是使用者引用的歷史會話上下文：", "目前使用者問題："},
}

func stripReferencedSessionPreamble(text string) string {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	for _, pair := range referencedSessionPreamble {
		if !strings.HasPrefix(trimmed, pair.header) {
			continue
		}
		body := trimmed[len(pair.header):]
		if index := strings.Index(body, pair.footer); index >= 0 {
			return body[index+len(pair.footer):]
		}
		// No footer: keep the remainder so quoted text is never dropped.
		return body
	}
	return text
}

const (
	selectedTextContextOpen  = "<reasonix-selected-chat-context>"
	selectedTextContextClose = "</reasonix-selected-chat-context>"
)

// collapseSelectedContextBlock replaces a trailing selected-chat-context block
// with the same compact labels the composer appended to the regular message
// display form. A malformed or non-trailing block is stripped entirely rather
// than leaking provider-facing framing into the transcript.
func collapseSelectedContextBlock(text string) string {
	openIndex := strings.LastIndex(text, selectedTextContextOpen)
	if openIndex < 0 {
		return text
	}
	bodyStart := openIndex + len(selectedTextContextOpen)
	closeIndex := strings.Index(text[bodyStart:], selectedTextContextClose)
	if closeIndex < 0 {
		return text
	}
	closeEnd := bodyStart + closeIndex + len(selectedTextContextClose)
	// The composer owns this block as the final submit suffix; a non-empty
	// tail means the marker is quoted user text, not current-message metadata.
	if strings.TrimSpace(text[closeEnd:]) != "" {
		return text
	}
	body := text[bodyStart : bodyStart+closeIndex]
	payloadStart := strings.IndexByte(body, '[')
	if payloadStart < 0 {
		return strings.TrimSpace(text[:openIndex])
	}
	var entries []struct {
		Text string `json:"text"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(body[payloadStart:]), &entries); err != nil {
		return strings.TrimSpace(text[:openIndex])
	}
	base := strings.TrimSpace(text[:openIndex])
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Text == "" {
			continue
		}
		labels = append(labels, selectedContextLabel(entry.Text, entry.Path))
	}
	if len(labels) == 0 {
		return base
	}
	return base + " " + strings.Join(labels, " ")
}

// selectedContextLabel renders one selection as the composer's display label:
// "[Chat: snippet]" or "[Code: basename → snippet]". Snippets are whitespace-
// collapsed and truncated at 40 runes with a "…" suffix; "]" is escaped to
// "］" so labels remain an unambiguous trailing suffix.
func selectedContextLabel(text, path string) string {
	if path == "" {
		return "[Chat: " + selectionLabelPart(text) + "]"
	}
	return "[Code: " + selectionLabelPart(selectedContextName(path)) + " → " + selectionLabelPart(text) + "]"
}

func selectedContextName(path string) string {
	clean := strings.TrimRight(path, "/\\")
	if slash := strings.LastIndexAny(clean, "/\\"); slash >= 0 {
		if name := clean[slash+1:]; name != "" {
			return name
		}
	}
	return path
}

func selectionLabelPart(value string) string {
	const maxRunes = 40
	text := strings.Join(strings.Fields(value), " ")
	escaped := strings.ReplaceAll(text, "]", "\uFF3D")
	if len([]rune(escaped)) <= maxRunes {
		return escaped
	}
	cut := []rune(escaped)[:maxRunes-1]
	return strings.TrimSpace(string(cut)) + "..."
}
