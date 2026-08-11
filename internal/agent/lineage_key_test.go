package agent

import (
	"testing"

	"reasonix/internal/provider"
)

func TestLineageKeyCompatibleNativeSuffix(t *testing.T) {
	current := "ws|sess|model"
	stored := current + "|context-editing-native-anthropic"
	got, ok := lineageKeyCompatible(stored, current)
	if !ok || got != current {
		t.Fatalf("lineageKeyCompatible(%q,%q)=(%q,%v), want (%q,true)", stored, current, got, ok, current)
	}
	if _, ok := lineageKeyCompatible("other|sess|model", current); ok {
		t.Fatal("foreign lineage must not match")
	}
	if got, ok := lineageKeyCompatible(current, current); !ok || got != current {
		t.Fatalf("exact key: got (%q,%v)", got, ok)
	}
}

func TestProjectionValidAcceptsLegacyNativeKey(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "u"},
	}
	hash := coveredPrefixHash(msgs, len(msgs))
	st := CompactionState{
		PromptCacheKey: "ws|s|m|context-editing-native-x",
		Projection: ContextProjection{
			Messages:          msgs,
			CoveredCount:      len(msgs),
			CoveredPrefixHash: hash,
			ProjectionVersion: 1,
		},
		TranscriptVersion: 1,
	}
	if !projectionValid(st, msgs, 1, "ws|s|m") {
		t.Fatal("projectionValid rejected legacy native lineage key")
	}
}
