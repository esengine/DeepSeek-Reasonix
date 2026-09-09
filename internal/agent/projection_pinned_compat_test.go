package agent

import (
	"testing"

	"reasonix/internal/provider"
)

func TestLegacyProjectionCannotCoverPinnedContextRevision(t *testing.T) {
	canonical := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "<pinned_context_revision>private pinned body</pinned_context_revision>"},
		{Role: provider.RoleUser, Content: "visible question"},
	}
	legacy := CompactionState{
		SchemaVersion: compactionStateSchemaV3,
		Projection: ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleUser, Content: "legacy summary"}},
			CoveredCount:      len(canonical),
			CoveredPrefixHash: coveredPrefixHash(canonical, len(canonical)),
		},
	}
	if projectionContentValid(legacy, canonical) {
		t.Fatal("legacy projection covering a pinned revision must be rebuilt")
	}

	withoutRevision := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "visible question"},
	}
	legacy.Projection.CoveredCount = len(withoutRevision)
	legacy.Projection.CoveredPrefixHash = coveredPrefixHash(withoutRevision, len(withoutRevision))
	if !projectionContentValid(legacy, withoutRevision) {
		t.Fatal("legacy projection without pinned revisions should remain compatible")
	}
}
