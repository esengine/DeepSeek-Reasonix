package textutil

import "testing"

func TestNaturalLess(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		// Basic numeric comparison
		{"file2", "file10", true},
		{"file10", "file2", false},
		{"2", "10", true},
		{"10", "2", false},
		{"a2b", "a10b", true},
		{"a10b", "a2b", false},

		// Chinese filenames (#6042)
		{"第1章.md", "第10章.md", true},
		{"第10章.md", "第1章.md", false},
		{"第1卷.md", "第2卷.md", true},
		{"第2卷.md", "第10卷.md", true},
		{"第19卷.md", "第20卷.md", true},

		// Equal strings
		{"abc", "abc", false},
		{"file1", "file1", false},

		// Case insensitive
		{"ABC", "abc", false},
		{"File1", "file2", true},
		{"FILE10", "file2", false},

		// Leading zeros
		{"file01", "file1", false}, // "01"(2) vs "1"(1): same number, shorter string first → "file1" < "file01"
		{"file1", "file01", true},
		{"file01", "file001", true}, // both 1, length 2 vs 3 → "file01" < "file001"

		// Mixed digit/non-digit: digits first
		{"1a", "a1", true},

		// Prefix relationship (shorter first)
		{"abc", "abcd", true},
		{"abcd", "abc", false},
		{"a", "a1", true},
		{"a1", "a", false},

		// Multi-number segments
		{"v1.2.3", "v1.10.0", true},
		{"v1.10.0", "v1.2.3", false},
		{"a1b2c3", "a1b10c3", true},

		// Edge: empty
		{"", "a", true},
		{"a", "", false},
		{"", "", false},

		// Version-like strings
		{"v1.9.0", "v1.10.0", true},
		{"v1.10.0", "v1.9.0", false},

		// Real-world filenames
		{"report-2.pdf", "report-10.pdf", true},
		{"img_1.png", "img_10.png", true},
		{"log.1", "log.10", true},
		{"test_001.go", "test_002.go", true},

		// Numbers at different positions
		{"abc10def", "abc2def", false},
		{"abc2def", "abc10def", true},
	}

	for _, tt := range tests {
		got := NaturalLess(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("NaturalLess(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestNaturalLessSort(t *testing.T) {
	// Verify the ordering is transitive across a full sort.
	items := []string{
		"第10卷.md", "第1卷.md", "第2卷.md",
		"第20卷.md", "第11卷.md", "第19卷.md",
	}
	expected := []string{
		"第1卷.md", "第2卷.md", "第10卷.md",
		"第11卷.md", "第19卷.md", "第20卷.md",
	}

	sorted := make([]string, len(items))
	copy(sorted, items)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if NaturalLess(sorted[j], sorted[i]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	for i := range sorted {
		if sorted[i] != expected[i] {
			t.Errorf("sorted[%d] = %q, want %q; full sorted: %v", i, sorted[i], expected[i], sorted)
			break
		}
	}
}
