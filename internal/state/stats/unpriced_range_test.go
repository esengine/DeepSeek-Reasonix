package stats

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestATotalCarriesItsUnpricedCount reads the query result the way a client
// does — through the JSON it emits — so the fact is pinned at the wire, not in
// a struct field a later change could quietly stop filling.
func TestATotalCarriesItsUnpricedCount(t *testing.T) {
	out, err := json.Marshal(RangeStats{
		CostIncomplete:    true,
		UnpricedCalls:     2,
		IncompleteReasons: []string{"missing_price_or_usage", "no_price"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	for _, want := range []string{`"unpricedCalls":2`, `"incompleteReasons":["missing_price_or_usage","no_price"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("a total does not say how many calls it could not price: %s missing from %s", want, got)
		}
	}
}
