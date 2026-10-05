package agent

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/provider"
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
	hash := sessionstore.CoveredPrefixHash(msgs, len(msgs))
	st := sessionstore.CompactionState{
		PromptCacheKey: "ws|s|m|context-editing-native-x",
		Projection: sessionstore.ContextProjection{
			Messages:          msgs,
			CoveredCount:      len(msgs),
			CoveredPrefixHash: hash,
			ProjectionVersion: 1,
		},
		TranscriptVersion: 1,
	}
	if !projectionValid(st, msgs, "ws|s|m", nil) {
		t.Fatal("projectionValid rejected legacy native lineage key")
	}
}
