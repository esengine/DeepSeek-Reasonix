package agent

import (
	"testing"

	"reasonix/internal/provider"
)

// storedVersions are transcript-version values a sidecar can carry when a new
// process asks about it: the one it was written under, the counter a reload
// starts from, and values from neither. A verdict that moves across this set is
// gating durable reuse on in-process bookkeeping.
var storedVersions = []uint64{0, 1, 18, 4096}

func authorityMessages() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "done"},
	}
}

func authorityState(covered int, hash string, version uint64) CompactionState {
	return CompactionState{
		TranscriptVersion: version,
		PromptCacheKey:    "ws|sess|model",
		Projection: ContextProjection{
			Messages:          []provider.Message{{Role: provider.RoleSystem, Content: "digest"}},
			TranscriptVersion: version,
			CoveredCount:      covered,
			CoveredPrefixHash: hash,
		},
	}
}

// TestProjectionValidityIsCoveredPrefixIdentity pins the contract the covered
// hash carries alone. Every case runs under every stored transcript version:
// the verdict is the same one each time, or the hash is not the authority.
func TestProjectionValidityIsCoveredPrefixIdentity(t *testing.T) {
	msgs := authorityMessages()
	grown := append(append([]provider.Message(nil), msgs...),
		provider.Message{Role: provider.RoleUser, Content: "next"})
	tailRewritten := append([]provider.Message(nil), grown...)
	tailRewritten[3].Content = "next, rewritten"
	edited := append([]provider.Message(nil), msgs...)
	edited[1].Content = "task-EDITED"

	full := coveredPrefixHash(msgs, len(msgs))
	cases := []struct {
		name    string
		covered int
		hash    string
		against []provider.Message
		want    bool
	}{
		// The whole transcript is what the digest folded, and no tail is left to
		// fall back on. This is the case a restart used to lose.
		{"whole transcript covered", len(msgs), full, msgs, true},
		{"append-only growth", len(msgs), full, grown, true},
		{"tail rewritten past the fold", len(msgs), full, tailRewritten, true},
		{"covered row edited", len(msgs), full, edited, false},
		{"covered count past the transcript", len(msgs) + 1, full, msgs, false},
		{"legacy sidecar without a hash", len(msgs), "", msgs, false},
		{"covered count of zero", 0, full, msgs, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, version := range storedVersions {
				st := authorityState(tc.covered, tc.hash, version)
				if got := projectionValid(st, tc.against, "ws|sess|model", nil); got != tc.want {
					t.Fatalf("stored transcript version %d: valid = %v, want %v", version, got, tc.want)
				}
			}
		})
	}
}

// TestProjectionValidityIgnoresTranscriptVersion states the same rule as a
// property rather than a table: the transcript a projection is asked about
// decides the verdict, and the counter the sidecar happens to carry does not.
func TestProjectionValidityIgnoresTranscriptVersion(t *testing.T) {
	msgs := authorityMessages()
	hash := coveredPrefixHash(msgs, len(msgs))
	first := projectionValid(authorityState(len(msgs), hash, storedVersions[0]), msgs, "ws|sess|model", nil)
	if !first {
		t.Fatal("a projection whose covered prefix still matches must be valid")
	}
	for _, version := range storedVersions[1:] {
		if got := projectionValid(authorityState(len(msgs), hash, version), msgs, "ws|sess|model", nil); got != first {
			t.Fatalf("stored transcript version %d changed the verdict to %v", version, got)
		}
	}
}
