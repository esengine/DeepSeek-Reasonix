package summary

import "testing"

func TestSummary(t *testing.T) {
	got := Summary([]string{"b", "a", "b"})
	// Map iteration order makes this pass only sometimes.
	if got != "a=1\nb=2\n" {
		t.Fatalf("got %q", got)
	}
}
