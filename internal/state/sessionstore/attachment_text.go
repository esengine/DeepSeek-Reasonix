package sessionstore

import (
	"path/filepath"
	"strings"

	"reasonix/internal/contract/provider"
)

// AttachmentDir is the workspace-relative directory the host stores pasted
// bytes in. A token naming it addresses the host's own copy, not a file the
// person pointed at, so it is host chrome in a line rather than their words.
const AttachmentDir = ".reasonix/attachments/"

// IsAttachmentToken reports whether tok — the part after '@' — names the
// host's attachment store. The turn parser and the display projection both
// read it here, so the two cannot disagree about which tokens are the host's.
func IsAttachmentToken(tok string) bool {
	return strings.HasPrefix(filepath.ToSlash(tok), AttachmentDir)
}

// StripAttachmentTokens removes the @tokens that address stored attachments,
// and the separator one of them would otherwise leave doubled. What is left is
// what the person wrote: the picture beside the message is the attachment.
func StripAttachmentTokens(text string) string {
	if !strings.Contains(text, AttachmentDir) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	var last byte
	for i := 0; i < len(text); {
		if text[i] != '@' {
			b.WriteByte(text[i])
			last = text[i]
			i++
			continue
		}
		end := i + 1
		for end < len(text) && !isRefSpace(text[end]) {
			end++
		}
		if !IsAttachmentToken(strings.TrimRight(text[i+1:end], ".,;!?)]}")) {
			b.WriteString(text[i:end])
			last = text[end-1]
			i = end
			continue
		}
		// Dropping the token leaves the space on each side of it adjacent; the
		// one after it goes, so the sentence keeps a single separator.
		if next := end + 1; next < len(text) && isRefSpace(text[end]) &&
			(b.Len() == 0 || isRefSpace(last)) {
			i = next
			continue
		}
		i = end
	}
	return strings.TrimSpace(b.String())
}

// UserDisplayText is what a persisted user turn says on screen: the person's
// own words, with attachments shown as themselves. UserMessageText keeps the
// tokens because turn identity reads the line the model was given.
func UserDisplayText(msg provider.Message) string {
	return StripAttachmentTokens(UserMessageText(msg))
}

func isRefSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}
