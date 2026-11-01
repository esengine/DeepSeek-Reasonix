package builtin

import "testing"

func TestByteCountMovesToNextUnitWhenRoundingReachesIt(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1_048_524, "1023.9 KB"},
		{1_048_525, "1.0 MB"},
		{1_048_575, "1.0 MB"},
		{1 << 20, "1.0 MB"},
	}
	for _, tt := range tests {
		if got := byteCount(tt.n); got != tt.want {
			t.Errorf("byteCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
