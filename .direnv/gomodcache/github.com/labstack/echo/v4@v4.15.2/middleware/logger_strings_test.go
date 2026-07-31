// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteJSONSafeString(t *testing.T) {
	testCases := []struct {
		name      string
		whenInput string
		expect    string
		expectN   int
	}{
		// Basic cases
		{
			name:      "empty string",
			whenInput: "",
			expect:    "",
			expectN:   0,
		},
		{
			name:      "simple ASCII without special chars",
			whenInput: "hello",
			expect:    "hello",
			expectN:   5,
		},
		{
			name:      "single character",
			whenInput: "a",
			expect:    "a",
			expectN:   1,
		},
		{
			name:      "alphanumeric",
			whenInput: "Hello123World",
			expect:    "Hello123World",
			expectN:   13,
		},

		// Special character escaping
		{
			name:      "backslash",
			whenInput: `path\to\file`,
			expect:    `path\\to\\file`,
			expectN:   14,
		},
		{
			name:      "double quote",
			whenInput: `say "hello"`,
			expect:    `say \"hello\"`,
			expectN:   13,
		},
		{
			name:      "backslash and quote combined",
			whenInput: `a\b"c`,
			expect:    `a\\b\"c`,
			expectN:   7,
		},
		{
			name:      "single backslash",
			whenInput: `\`,
			expect:    `\\`,
			expectN:   2,
		},
		{
			name:      "single quote",
			whenInput: `"`,
			expect:    `\"`,
			expectN:   2,
		},

		// Control character escaping
		{
			name:      "backspace",
			whenInput: "hello\bworld",
			expect:    `hello\bworld`,
			expectN:   12,
		},
		{
			name:      "form feed",
			whenInput: "hello\fworld",
			expect:    `hello\fworld`,
			expectN:   12,
		},
		{
			name:      "newline",
			whenInput: "hello\nworld",
			expect:    `hello\nworld`,
			expectN:   12,
		},
		{
			name:      "carriage return",
			whenInput: "hello\rworld",
			expect:    `hello\rworld`,
			expectN:   12,
		},
		{
			name:      "tab",
			whenInput: "hello\tworld",
			expect:    `hello\tworld`,
			expectN:   12,
		},
		{
			name:      "multiple newlines",
			whenInput: "line1\nline2\nline3",
			expect:    `line1\nline2\nline3`,
			expectN:   19,
		},

		// Low control characters (< 0x20)
		{
			name:      "null byte",
			whenInput: "hello\x00world",
			expect:    `hello\u0000world`,
			expectN:   16,
		},
		{
			name:      "control character 0x01",
			whenInput: "test\x01value",
			expect:    `test\u0001value`,
			expectN:   15,
		},
		{
			name:      "control character 0x0e",
			whenInput: "test\x0evalue",
			expect:    `test\u000evalue`,
			expectN:   15,
		},
		{
			name:      "control character 0x1f",
			whenInput: "test\x1fvalue",
			expect:    `test\u001fvalue`,
			expectN:   15,
		},
		{
			name:      "multiple control characters",
			whenInput: "\x00\x01\x02",
			expect:    `\u0000\u0001\u0002`,
			expectN:   18,
		},

		// UTF-8 handling
		{
			name:      "valid UTF-8 Chinese",
			whenInput: "hello 世界",
			expect:    "hello 世界",
			expectN:   12,
		},
		{
			name:      "valid UTF-8 emoji",
			whenInput: "party 🎉 time",
			expect:    "party 🎉 time",
			expectN:   15,
		},
		{
			name:      "mixed ASCII and UTF-8",
			whenInput: "Hello世界123",
			expect:    "Hello世界123",
			expectN:   14,
		},
		{
			name:      "UTF-8 with special chars",
			whenInput: "世界\n\"test\"",
			expect:    `世界\n\"test\"`,
			expectN:   16,
		},

		// Invalid UTF-8
		{
			name:      "invalid UTF-8 sequence",
			whenInput: "hello\xff\xfeworld",
			expect:    `hello\ufffd\ufffdworld`,
			expectN:   22,
		},
		{
			name:      "incomplete UTF-8 sequence",
			whenInput: "test\xc3value",
			expect:    `test\ufffdvalue`,
			expectN:   15,
		},

		// Complex mixed cases
		{
			name:      "all common escapes",
			whenInput: "tab\there\nquote\"backslash\\",
			expect:    `tab\there\nquote\"backslash\\`,
			expectN:   29,
		},
		{
			name:      "mixed controls and UTF-8",
			whenInput: "hello\t世界\ntest\"",
			expect:    `hello\t世界\ntest\"`,
			expectN:   21,
		},
		{
			name:      "all control characters",
			whenInput: "\b\f\n\r\t",
			expect:    `\b\f\n\r\t`,
			expectN:   10,
		},
		{
			name:      "control and low ASCII",
			whenInput: "a\nb\x00c",
			expect:    `a\nb\u0000c`,
			expectN:   11,
		},

		// Edge cases
		{
			name:      "starts with special char",
			whenInput: "\\start",
			expect:    `\\start`,
			expectN:   7,
		},
		{
			name:      "ends with special char",
			whenInput: "end\"",
			expect:    `end\"`,
			expectN:   5,
		},
		{
			name:      "consecutive special chars",
			whenInput: "\\\\\"\"",
			expect:    `\\\\\"\"`,
			expectN:   8,
		},
		{
			name:      "only special characters",
			whenInput: "\"\\\n\t",
			expect:    `\"\\\n\t`,
			expectN:   8,
		},
		{
			name:      "spaces and punctuation",
			whenInput: "Hello, World! How are you?",
			expect:    "Hello, World! How are you?",
			expectN:   26,
		},
		{
			name:      "JSON-like string",
			whenInput: "{\"key\":\"value\"}",
			expect:    `{\"key\":\"value\"}`,
			expectN:   19,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			n, err := writeJSONSafeString(buf, tt.whenInput)

			assert.NoError(t, err)
			assert.Equal(t, tt.expect, buf.String())
			assert.Equal(t, tt.expectN, n)
		})
	}
}

func BenchmarkWriteJSONSafeString(b *testing.B) {
	testCases := []struct {
		name  string
		input string
	}{
		{"simple", "hello world"},
		{"with escapes", "tab\there\nquote\"backslash\\"},
		{"utf8", "hello 世界 🎉"},
		{"mixed", "Hello\t世界\ntest\"value\\path"},
		{"long simple", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"},
		{"long complex", "line1\nline2\tline3\"quote\\slash\x00null世界🎉"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			buf := &bytes.Buffer{}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				writeJSONSafeString(buf, tc.input)
			}
		})
	}
}
