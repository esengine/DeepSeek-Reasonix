package magic

import (
	"io/ioutil"
	"testing"
)

func TestMagic(t *testing.T) {
	tCases := []struct {
		name     string
		detector Detector
		raw      string
		limit    uint32
		res      bool
	}{
		{
			name:     "incomplete JSON, limit 0",
			detector: JSON,
			raw:      `["an incomplete JSON array`,
			limit:    0,
			res:      false,
		},
		{
			name:     "incomplete JSON, limit 10",
			detector: JSON,
			raw:      `["an incomplete JSON array`,
			limit:    10,
			res:      true,
		},
		{
			name:     "basic JSON data type null",
			detector: JSON,
			raw:      `null`,
			limit:    10,
			res:      false,
		},
		{
			name:     "basic JSON data type string",
			detector: JSON,
			raw:      `"abc"`,
			limit:    10,
			res:      false,
		},
		{
			name:     "basic JSON data type integer",
			detector: JSON,
			raw:      `120`,
			limit:    10,
			res:      false,
		},
		{
			name:     "basic JSON data type float",
			detector: JSON,
			raw:      `.120`,
			limit:    10,
			res:      false,
		},
		{
			name:     "NdJSON with basic data types",
			detector: NdJSON,
			raw:      "1\nnull\n\"foo\"\n0.1",
			limit:    10,
			res:      false,
		},
		{
			name:     "NdJSON with basic data types and empty object",
			detector: NdJSON,
			raw:      "1\n2\n3\n{}",
			limit:    10,
			res:      true,
		},
		{
			name:     "NdJSON with empty objects types",
			detector: NdJSON,
			raw:      "{}\n{}\n{}",
			limit:    10,
			res:      true,
		},
	}
	for _, tt := range tCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.detector([]byte(tt.raw), tt.limit); got != tt.res {
				t.Errorf("expected: %t; got: %t", tt.res, got)
			}
		})
	}
}

func TestDropLastLine(t *testing.T) {
	dropTests := []struct {
		raw   string
		cutAt uint32
		res   string
	}{
		{"", 0, ""},
		{"", 1, ""},
		{"å", 2, "å"},
		{"\n", 0, "\n"},
		{"\n", 1, "\n"},
		{"\n\n", 1, "\n"},
		{"\n\n", 3, "\n\n"},
		{"a\n\n", 3, "a\n"},
		{"\na\n", 3, "\na"},
		{"å\n\n", 5, "å\n\n"},
		{"\nå\n", 5, "\nå\n"},
	}
	for i, tt := range dropTests {
		gotR := dropLastLine([]byte(tt.raw), tt.cutAt)
		got, _ := ioutil.ReadAll(gotR)
		if got := string(got); got != tt.res {
			t.Errorf("dropLastLine %d error: expected %q; got %q", i, tt.res, got)
		}
	}
}

func BenchmarkSrt(b *testing.B) {
	const subtitle = `1
00:02:16,612 --> 00:02:19,376
Senator, we're making
our final approach into Coruscant.

`
	for i := 0; i < b.N; i++ {
		Srt([]byte(subtitle), 0)
	}
}
