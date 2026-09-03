package pty

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// CleanTerminalOutput strips ANSI escape sequences, color codes, and normalizes
// carriage returns / newlines into clean, readable text for LLM consumption.
func CleanTerminalOutput(raw string) string {
	if raw == "" {
		return ""
	}
	// 1. Strip ANSI escape sequences (CSI, OSC, SGR color codes)
	s := ansi.Strip(raw)

	// 2. Normalize Windows/Unix line endings
	s = strings.ReplaceAll(s, "\r\n", "\n")

	// 3. Handle standalone carriage returns (\r) without \n (e.g. progress bars, line redraws)
	if strings.Contains(s, "\r") {
		s = processCarriageReturns(s)
	}

	return s
}

// processCarriageReturns processes standalone '\r' characters like a real terminal,
// overwriting the current line from column 0.
func processCarriageReturns(s string) string {
	lines := strings.Split(s, "\n")
	for idx, line := range lines {
		if !strings.Contains(line, "\r") {
			continue
		}
		parts := strings.Split(line, "\r")
		var current []rune
		for _, part := range parts {
			r := []rune(part)
			if len(r) >= len(current) {
				current = r
			} else {
				// Overwrite the prefix of the current line
				copy(current[:len(r)], r)
			}
		}
		lines[idx] = string(current)
	}
	return strings.Join(lines, "\n")
}

// CleanBytes is a helper to clean and normalize a byte slice directly.
func CleanBytes(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	// Fast path: if valid UTF-8 without control codes except \n, return string directly
	if !bytes.Contains(raw, []byte{0x1b}) && !bytes.Contains(raw, []byte{'\r'}) {
		return string(raw)
	}
	return CleanTerminalOutput(string(raw))
}
