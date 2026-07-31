package words

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
			name:          "found_after_regular_chars",
			properties:    _Numeric,
			data:          []byte("1ab"), // Changed from "a1b" - should find '1' immediately
			atEOF:         true,
			expectAdvance: 0,
			expectMore:    false,
			description:   "Should find numeric character immediately",
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
			name:          "found_hebrew_letter",
			properties:    _HebrewLetter,
			data:          []byte("א"), // Hebrew Aleph
			atEOF:         true,
			expectAdvance: 0,
			expectMore:    false,
			description:   "Should find Hebrew letter immediately",
		},

		// Basic not found cases
		{
			name:          "not_found_definitive",
			properties:    _Numeric,
			data:          []byte("abc"),
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Should not find numeric in letters, definitive at EOF",
		},
		{
			name:          "not_found_after_non_matching",
			properties:    _Numeric,
			data:          []byte("xyz"),
			atEOF:         false,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Should not find numeric after seeing non-matching letter 'x' (immediate lookup)",
		},
		{
			name:          "not_found_immediate_non_match",
			properties:    _Numeric,
			data:          []byte("a123"), // 'a' doesn't match, should return not found immediately
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Should return not found immediately when first char doesn't match",
		},
		{
			name:          "not_found_after_ignored_then_non_matching",
			properties:    _Numeric,
			data:          []byte("\u200dabc"), // ZWJ + letters
			atEOF:         true,
			expectAdvance: notfound,
			expectMore:    false,
			description:   "Should skip ZWJ but not find numeric in letters",
		},

		// Edge cases with empty data
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

		// Cases with only ignored characters
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

		// Incomplete rune cases
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

		// Mixed ignored and non-ignored
		{
			name:          "ignored_then_found",
			properties:    _AHLetter,
			data:          []byte("\u200d\u034f" + "Hello"), // ZWJ + Combining Grapheme Joiner + letters
			atEOF:         true,
			expectAdvance: 5, // Skip ZWJ (3 bytes) + CGJ (2 bytes) = 5 bytes
			expectMore:    false,
			description:   "Should skip multiple ignored chars and find letter",
		},

		// Multiple properties
		{
			name:          "find_any_of_multiple_properties",
			properties:    _Numeric | _AHLetter,
			data:          []byte("A123"), // Changed: should find 'A' immediately
			atEOF:         true,
			expectAdvance: 0,
			expectMore:    false,
			description:   "Should find letter immediately when looking for numeric OR letter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advance, more := subsequent(tt.properties, tt.data, tt.atEOF)

			if advance != tt.expectAdvance {
				t.Errorf("advance = %v, expected %v\nDescription: %s", advance, tt.expectAdvance, tt.description)
			}
			if more != tt.expectMore {
				t.Errorf("more = %v, expected %v\nDescription: %s", more, tt.expectMore, tt.description)
			}
		})
	}
}

// Test the interaction between subsequent and actual word boundary rules
func TestSubsequentWordBoundaryIntegration(t *testing.T) {
	// Test cases that mirror real word boundary scenarios

	// WB6: AHLetter × (MidLetter | MidNumLetQ) AHLetter
	// Looking for AHLetter after seeing MidLetter
	t.Run("wb6_scenario", func(t *testing.T) {
		// Simulate: we have "word'" and looking ahead for letter after apostrophe
		data := []byte("test") // Should find 't' immediately
		advance, more := subsequent(_AHLetter, data, true)
		if advance != 0 || more != false {
			t.Errorf("Expected to find AHLetter immediately, got advance=%d, more=%t", advance, more)
		}
	})

	// WB7b: HebrewLetter × DoubleQuote HebrewLetter
	// Looking for HebrewLetter after DoubleQuote
	t.Run("wb7b_scenario", func(t *testing.T) {
		data := []byte("א") // Hebrew Aleph
		advance, more := subsequent(_HebrewLetter, data, true)
		if advance != 0 || more != false {
			t.Errorf("Expected to find HebrewLetter immediately, got advance=%d, more=%t", advance, more)
		}
	})

	// WB12: Numeric × (MidNum | MidNumLetQ) Numeric
	// Looking for Numeric after MidNum
	t.Run("wb12_scenario", func(t *testing.T) {
		data := []byte("5") // Should find numeric immediately
		advance, more := subsequent(_Numeric, data, true)
		if advance != 0 || more != false {
			t.Errorf("Expected to find Numeric immediately, got advance=%d, more=%t", advance, more)
		}
	})
}
