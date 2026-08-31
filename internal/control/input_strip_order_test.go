package control

import "testing"

// TestStripComposePrefixesOrdering pins the #7804 regression: the plan marker
// is prepended after the transient blocks, so blocks only become leading once
// the marker strips — a single pass left the whole notice chain in the rewind
// picker's turn label.
func TestStripComposePrefixesOrdering(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "marker before transient blocks strips both",
			input: PlanModeMarker + "\n\n<reasoning-language>zh</reasoning-language>\n\n<memory-update>\nsaved\n</memory-update>\n\nfix login",
			want:  "fix login",
		},
		{
			name:  "marker between transient blocks strips all",
			input: "<background-jobs>\n1 running\n</background-jobs>\n\n" + PlanModeMarker + "\n\n<active-goal>ship it</active-goal>\n\nkeep going",
			want:  "keep going",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripComposePrefixes(tt.input); got != tt.want {
				t.Fatalf("StripComposePrefixes() = %q, want %q", got, tt.want)
			}
		})
	}
}
