package event

import "testing"

// One rule, one place. A receipt shown in the window and swallowed in the
// terminal is the same turn described two ways, which is the drift this
// predicate exists to prevent.
func TestReceiptSaysSomething(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    *CompletionReceipt
		want bool
	}{
		{"nil", nil, false},
		{"a clean verdict is sourced, not silent", &CompletionReceipt{Verdict: "done"}, true},
		{"a gap is the whole point", &CompletionReceipt{Verdict: "partial", Gaps: []ReceiptGap{{Kind: "unreviewed_change"}}}, true},
		// Declarations used to ride in as gaps. They no longer do, so a receipt
		// carrying only one still has to be worth showing or the honest answer
		// disappears from every surface at once.
		{"a declared caveat", &CompletionReceipt{Verdict: "incomplete", Unverified: []string{"UI never exercised"}}, true},
		{"a declared risk", &CompletionReceipt{Verdict: "incomplete", Risks: []string{"one-way migration"}}, true},
		{"nothing judged", &CompletionReceipt{Verdict: "incomplete"}, false},
	} {
		if got := tc.r.SaysSomething(); got != tc.want {
			t.Errorf("%s: SaysSomething() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
