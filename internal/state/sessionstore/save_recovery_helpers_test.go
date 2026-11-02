package sessionstore

import (
	"bytes"
	"os"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func TestInheritCompactionStateKeepsProjectionButResetsSessionRuntimeState(t *testing.T) {
	canonical := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "folded question"},
		{Role: provider.RoleAssistant, Content: "live answer"},
	}
	for _, tc := range []struct {
		name        string
		receipt     *ContextMaintenanceReceipt
		legacyBlock bool
		wantReceipt bool
	}{
		{
			name:        "applied receipt is durable metadata",
			receipt:     &ContextMaintenanceReceipt{Status: "applied", Action: "summary", SummaryHash: "digest"},
			wantReceipt: true,
		},
		{
			name:    "blocked receipt is source-session suppression",
			receipt: &ContextMaintenanceReceipt{Status: "blocked", Action: "summary", Reason: "source failed"},
		},
		{
			name:        "legacy blocked fields are source-session suppression",
			legacyBlock: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testenv.TempDir(t)
			source := NewSessionPath(dir, "source")
			target := NewSessionPath(dir, "target")
			st := CompactionState{
				SchemaVersion:  CompactionStateSchemaCurrent,
				PromptCacheKey: "source|lineage",
				Generation:     7,
				Recall:         RecallLedger{Generation: 7, SpentTokens: 900},
				LastReceipt:    tc.receipt,
				Projection: ContextProjection{
					Messages:          []provider.Message{{Role: provider.RoleUser, Content: "<compaction-summary>\nsummary\n</compaction-summary>"}},
					ProjectionVersion: 4,
					CoveredCount:      2,
					CoveredPrefixHash: CoveredPrefixHash(canonical, 2),
				},
				UpdatedAt: time.Now().UTC(),
			}
			if tc.legacyBlock {
				st.BlockedInputHash = "source input"
				st.BlockedReason = "source blocked"
			}
			if err := SaveCompactionState(source, st); err != nil {
				t.Fatal(err)
			}
			sourceBefore, err := os.ReadFile(ContextStatePath(source))
			if err != nil {
				t.Fatal(err)
			}

			if !InheritCompactionState(source, target, canonical) {
				t.Fatal("valid projection was not inherited")
			}
			got, ok, err := LoadCompactionState(target)
			if err != nil || !ok {
				t.Fatalf("load inherited state: ok=%v err=%v", ok, err)
			}
			if got.Projection.CoveredCount != 2 || got.Projection.ProjectionVersion != 4 ||
				got.Projection.CoveredPrefixHash != CoveredPrefixHash(canonical, 2) {
				t.Fatalf("projection changed during inheritance: %+v", got.Projection)
			}
			if got.Generation != 7 || got.PromptCacheKey != "source|lineage" {
				t.Fatalf("durable lineage metadata changed: %+v", got)
			}
			if got.Recall != (RecallLedger{}) || got.BlockedInputHash != "" || got.BlockedReason != "" {
				t.Fatalf("source runtime state leaked into target: %+v", got)
			}
			if (got.LastReceipt != nil) != tc.wantReceipt {
				t.Fatalf("LastReceipt = %+v, wantReceipt=%v", got.LastReceipt, tc.wantReceipt)
			}
			sourceAfter, err := os.ReadFile(ContextStatePath(source))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sourceBefore, sourceAfter) {
				t.Fatal("inheritance modified the source sidecar")
			}
		})
	}
}
