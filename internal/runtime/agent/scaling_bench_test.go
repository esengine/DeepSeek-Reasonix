package agent

import (
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// longSession builds a canonical transcript of n turns already folded behind a
// projection, which is what a session looks like after many compactions.
func longSession(n int) *Agent {
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}
	for i := range n {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d: %s", i, strings.Repeat("please keep going. ", 20))},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a plain english sentence about the work. ", 40)},
		)
	}
	sess := &sessionstore.Session{Messages: msgs}
	a := New(nil, nil, sess, Options{ContextWindow: 128_000}, event.Discard)
	covered := len(msgs) - 4 // a fold leaves a short verbatim tail
	a.sess.win.compactionState = sessionstore.CompactionState{
		TranscriptVersion: sess.TranscriptVersion(),
		Projection: sessionstore.ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: SummaryTagOpen + "digest" + SummaryTagClose},
			},
			TranscriptVersion: sess.TranscriptVersion(),
			CoveredCount:      covered,
			CoveredPrefixHash: sessionstore.CoveredPrefixHash(msgs, covered),
		},
		PromptCacheKey: a.window().currentPromptCacheKey(),
		Generation:     1,
	}
	if got := len(a.window().modelVisibleMessages()); got > 8 {
		panic(fmt.Sprintf("fixture projection is not in force: %d visible messages", got))
	}
	return a
}

// modelVisibleMessages runs on every turn. If its cost tracks the whole
// canonical transcript rather than the projection, a long session pays for
// history the model never sees.
func BenchmarkModelVisibleMessages(b *testing.B) {
	for _, turns := range []int{100, 1_000, 5_000, 20_000} {
		a := longSession(turns)
		b.Run(fmt.Sprint(turns), func(b *testing.B) {
			for b.Loop() {
				if got := a.window().modelVisibleMessages(); len(got) == 0 {
					b.Fatal("no visible messages")
				}
			}
		})
	}
}

// The status-bar gauge reads on every render, not every turn.
func BenchmarkContextUsedTokens(b *testing.B) {
	for _, turns := range []int{1_000, 20_000} {
		a := longSession(turns)
		b.Run(fmt.Sprint(turns), func(b *testing.B) {
			for b.Loop() {
				a.ContextUsedTokens()
			}
		})
	}
}

// What one turn's prompt costs to assemble, which is the number that has to
// stay flat: the provider only ever sees the projection.
func BenchmarkPreparedRequestSize(b *testing.B) {
	for _, turns := range []int{1_000, 20_000} {
		a := longSession(turns)
		b.Run(fmt.Sprint(turns), func(b *testing.B) {
			for b.Loop() {
				a.window().estimatedPromptTokens(a.window().modelVisibleMessages())
			}
		})
	}
}
