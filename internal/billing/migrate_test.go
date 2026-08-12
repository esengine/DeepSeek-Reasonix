package billing

import "testing"

func TestMigrateLegacyUsageIdentityOnly(t *testing.T) {
	q := MigrateLegacyUsage(LegacyUsageRecord{SessionCost: 1.25, SessionCurrency: "USD"})
	if !q.CostComplete || !q.DisplayComplete || !q.Complete || q.Selected == nil {
		t.Fatalf("migrated quote = %+v", q)
	}
	if len(q.Valuations) != 1 || q.Valuations["USD"].Basis != BasisIdentity {
		t.Fatalf("migrated valuations = %+v", q.Valuations)
	}
}

func TestMigrateLegacyWipedZeroDoesNotGuess(t *testing.T) {
	q := MigrateLegacyUsage(LegacyUsageRecord{SessionCurrency: "CNY"})
	if q.CostComplete || q.Complete || q.DisplayStatus != DisplayStatusUnavailable || q.IncompleteReason != "legacy_wiped_or_zero" {
		t.Fatalf("wiped quote = %+v", q)
	}
}
