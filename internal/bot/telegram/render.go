package telegram

import (
	"strings"
	"unicode/utf8"
)

const maxMessageRunes = 4000

func splitText(text string) []string {
	if text == "" {
		return []string{""}
	}
	if utf8.RuneCountInString(text) <= maxMessageRunes {
		return []string{text}
	}

	chunks := make([]string, 0, (utf8.RuneCountInString(text)+maxMessageRunes-1)/maxMessageRunes)
	for text != "" {
		if utf8.RuneCountInString(text) <= maxMessageRunes {
			chunks = append(chunks, text)
			break
		}
		cut := runeByteIndex(text, maxMessageRunes)
		boundary := preferredBoundary(text[:cut])
		if boundary > 0 {
			cut = boundary
		}
		chunks = append(chunks, text[:cut])
		text = text[cut:]
		text = strings.TrimLeft(text, " \n")
	}
	return chunks
}

func runeByteIndex(s string, count int) int {
	for index := range s {
		if count == 0 {
			return index
		}
		count--
	}
	return len(s)
}

func preferredBoundary(s string) int {
	for _, boundary := range []string{"\n\n", "\n", " "} {
		if i := strings.LastIndex(s, boundary); i > 0 {
			return i + len(boundary)
		}
	}
	return 0
}

func normalizeGroupText(text, username string) (string, bool) {
	text = strings.TrimSpace(text)
	mention := "@" + strings.TrimPrefix(strings.ToLower(username), "@")
	if username != "" && strings.HasPrefix(strings.ToLower(text), mention) {
		return strings.TrimSpace(text[len(mention):]), true
	}
	return text, false
}

func normalizeAddressedCommand(text, username string) (string, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || username == "" || !strings.HasPrefix(fields[0], "/") {
		return text, false
	}
	suffix := "@" + strings.TrimPrefix(strings.ToLower(username), "@")
	if !strings.HasSuffix(strings.ToLower(fields[0]), suffix) {
		return text, false
	}
	command := fields[0][:len(fields[0])-len(suffix)]
	if command == "" || command == "/" {
		return text, false
	}
	fields[0] = command
	return strings.Join(fields, " "), true
}
