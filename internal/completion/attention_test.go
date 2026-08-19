package completion

import "testing"

func TestNeedsAttentionMatrix(t *testing.T) {
	tests := []struct {
		name string
		in   AttentionInput
		want bool
	}{
		{name: "scratch or quiet standard", in: AttentionInput{Verdict: "uncertain"}, want: false},
		{name: "standard unreviewed", in: AttentionInput{Verdict: "partial", GapKinds: []string{"unreviewed_change"}}, want: false},
		{name: "standard unverified", in: AttentionInput{Verdict: "partial", GapKinds: []string{"unverified_change"}}, want: false},
		{name: "delivery unverified", in: AttentionInput{Verdict: "partial", Floor: "delivery", GapKinds: []string{"unverified_change"}}, want: true},
		{name: "failed test", in: AttentionInput{Verdict: "partial", GapKinds: []string{"failed_verification"}}, want: true},
		{name: "blocked", in: AttentionInput{Verdict: "blocked"}, want: true},
		{name: "failed check count", in: AttentionInput{ChecksFailed: 1}, want: true},
		{name: "unbacked claim", in: AttentionInput{GapKinds: []string{"unbacked_claim"}}, want: true},
		{name: "required suppressed", in: AttentionInput{RequiredSuppressed: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsAttention(tt.in); got != tt.want {
				t.Fatalf("NeedsAttention(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
