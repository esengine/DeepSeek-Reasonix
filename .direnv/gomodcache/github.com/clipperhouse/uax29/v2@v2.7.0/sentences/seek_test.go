package sentences

import (
	"testing"
)

func TestSubsequent(t *testing.T) {
	tests := []struct {
		name          string
		properties    property
		data          []byte
		atEOF         bool
		expectAdvance int
		expectMore    bool
		description   string
	}{
		// Basic found cases
		{
			name:          "found_immediately",
			properties:    _Numeric,
			data:          []byte("123"),
			atEOF:         true,
			expectAdvance: 0,
			expectMore:    false,
			description:   "Should find numeric character at start",
		},
		{
			name:          "found_after_ignored_chars",
			properties:    _Numeric,
			data:          []byte("\u200d1"), // ZWJ + '1'
			atEOF:         true,
			expectAdvance: 3, // ZWJ is 3 bytes
			expectMore:    false,
			description:   "Should skip ignored ZWJ and find numeric",
		},
		{
			name:          "not_found_definitive",
			properties:    _Numeric,
			data:          []byte("abc"),
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Should not find numeric in letters",
		},
		{
			name:          "not_found_immediate_non_match",
			properties:    _Numeric,
			data:          []byte("a"),
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Should not find numeric in single letter",
		},
		{
			name:          "empty_data_at_eof",
			properties:    _Numeric,
			data:          []byte(""),
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Empty data at EOF should return not found",
		},
		{
			name:          "empty_data_not_at_eof",
			properties:    _Numeric,
			data:          []byte(""),
			atEOF:         false,
			expectAdvance: notfound,
			expectMore:    true,
			description:   "Empty data not at EOF should request more data",
		},
		{
			name:          "only_ignored_at_eof",
			properties:    _Numeric,
			data:          []byte("\u200d"), // ZWJ only
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Only ignored chars at EOF should return not found",
		},
		{
			name:          "only_ignored_not_at_eof",
			properties:    _Numeric,
			data:          []byte("\u200d"), // ZWJ only
			atEOF:         false,
			expectAdvance: notfound,
			expectMore:    true,
			description:   "Only ignored chars not at EOF should request more data",
		},
		{
			name:          "incomplete_rune_at_eof",
			properties:    _Numeric,
			data:          []byte{0xE2, 0x80}, // Incomplete ZWJ (needs 3 bytes)
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Incomplete rune at EOF should return not found",
		},
		{
			name:          "incomplete_rune_not_at_eof",
			properties:    _Numeric,
			data:          []byte{0xE2, 0x80}, // Incomplete ZWJ
			atEOF:         false,
			expectAdvance: notfound,
			expectMore:    true,
			description:   "Incomplete rune not at EOF should request more data",
		},
		{
			name:          "ignored_then_found",
			properties:    _Numeric,
			data:          []byte("\u200d\u200d1"), // ZWJ + ZWJ + '1'
			atEOF:         true,
			expectAdvance: 6, // Two ZWJs = 6 bytes
			expectMore:    false,
			description:   "Should skip multiple ignored chars and find numeric",
		},
		{
			name:          "find_any_of_multiple_properties",
			properties:    _Numeric | _Upper,
			data:          []byte("1"),
			atEOF:         true,
			expectAdvance: 0,
			expectMore:    false,
			description:   "Should find any matching property",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advance, more := subsequent(tt.properties, tt.data, tt.atEOF)
			if advance != tt.expectAdvance {
				t.Errorf("advance = %d, expected %d\nDescription: %s", advance, tt.expectAdvance, tt.description)
			}
			if more != tt.expectMore {
				t.Errorf("more = %v, expected %v\nDescription: %s", more, tt.expectMore, tt.description)
			}
		})
	}
}
