package sftpfs

import "unicode/utf8"

// Kind classifies file content for preview purposes.
type Kind int

const (
	// KindText is UTF-8 (or ASCII) text safe to render and edit.
	KindText Kind = iota
	// KindBinary contains NUL bytes or invalid UTF-8; not editable as text.
	KindBinary
)

const (
	// DefaultReadCap bounds a text preview to keep memory and transfer sane.
	DefaultReadCap = 4 << 20 // 4 MiB
	// sniffLen is how many leading bytes DetectKind inspects.
	sniffLen = 8 << 10 // 8 KiB
)

// DetectKind classifies a leading sample of file content. A NUL byte marks
// binary immediately; otherwise the sample must be valid UTF-8 (allowing a
// trailing rune truncated by the sample boundary).
func DetectKind(sample []byte) Kind {
	if len(sample) > sniffLen {
		sample = sample[:sniffLen]
	}
	for _, b := range sample {
		if b == 0 {
			return KindBinary
		}
	}
	if utf8.Valid(sample) {
		return KindText
	}
	// The sample may have split a multi-byte rune at the tail; retry without
	// the trailing partial rune before declaring binary.
	for i := 0; i < utf8.UTFMax-1 && i < len(sample); i++ {
		if utf8.Valid(sample[:len(sample)-1-i]) {
			return KindText
		}
	}
	return KindBinary
}
