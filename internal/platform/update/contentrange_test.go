package update

import "testing"

// The start is what proves a 206 continues the caller's download. Anything
// unparseable must answer false rather than a plausible-looking zero.
func TestRangeStartFromContentRange(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		wantOK bool
	}{
		{"bytes 200-999/1000", 200, true},
		{"bytes 0-1023/1024", 0, true},
		{"bytes 200-999/*", 200, true},
		{"", 0, false},
		{"bytes */1000", 0, false},
		{"pages 1-2/3", 0, false},
		{"bytes -5-9/10", 0, false},
	}
	for _, tc := range cases {
		got, ok := RangeStartFromContentRange(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("RangeStartFromContentRange(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestTotalFromContentRange(t *testing.T) {
	if got := TotalFromContentRange("bytes 200-999/1000"); got != 1000 {
		t.Errorf("total = %d, want 1000", got)
	}
	if got := TotalFromContentRange("bytes 200-999/*"); got != 0 {
		t.Errorf("unknown total = %d, want 0", got)
	}
}
