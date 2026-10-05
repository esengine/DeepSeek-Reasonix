package main

import "testing"

// The shapes the probe exists to tell apart, written out rather than run: a
// full arm takes three processes and a live kernel, which is the wrong place to
// find out that a verdict was computed backwards.

func historyOf(ids ...string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func entry(phase, origin string, states map[string]string) UIEntry {
	return UIEntry{Phase: phase, Origin: origin, States: states}
}

func TestAHistoryReadBackHoldsEveryRow(t *testing.T) {
	historical := historyOf("g", "g/1", "g/2")
	trace := []UIEntry{
		entry("loading", "reset", nil),
		entry("live", "snapshot", map[string]string{"g": "", "g/1": "completed", "g/2": "failed"}),
		entry("live", "delta", map[string]string{"g": "", "g/1": "completed", "g/2": "failed", "n/1": "running"}),
		entry("live", "delta", map[string]string{"g": "", "g/1": "completed", "g/2": "failed", "n/1": "completed"}),
	}
	first := firstSight(trace)
	for _, r := range []row{
		uiFirstPictureRow(trace),
		uiHistoryOriginRow(historical, first),
		uiGhostRow(trace, historical),
		uiLiveRow(historical, first),
		uiRebirthRow(trace),
		uiRepublishRow(historical, [][]string{{"n/1"}}),
	} {
		if r.Verdict != verdictHolds {
			t.Errorf("%s = %s (%s), want %s", r.Semantic, r.Verdict, r.After, verdictHolds)
		}
	}
}

// The defect the arm was built for: the same final state, assembled out of
// transitions. Every value row elsewhere in this probe would still be green.
func TestHistoryArrivingAsNewsIsCaught(t *testing.T) {
	historical := historyOf("g/1")
	trace := []UIEntry{
		entry("loading", "reset", nil),
		entry("live", "delta", map[string]string{"g/1": "running"}),
		entry("live", "delta", map[string]string{"g/1": "completed"}),
	}
	first := firstSight(trace)
	if got := uiFirstPictureRow(trace); got.Verdict != verdictViolated {
		t.Errorf("first picture = %s, want %s", got.Verdict, verdictViolated)
	}
	if got := uiHistoryOriginRow(historical, first); got.Verdict != verdictViolated {
		t.Errorf("history origin = %s, want %s", got.Verdict, verdictViolated)
	}
	if got := uiGhostRow(trace, historical); got.Verdict != verdictViolated {
		t.Errorf("ghost = %s, want %s", got.Verdict, verdictViolated)
	}
}

// A snapshot the view holds and a delta that names the same work again: the
// state never moves, and the kernel still said finished work is happening.
func TestRepublishedHistoryIsCaughtWithoutMovingTheState(t *testing.T) {
	historical := historyOf("g/1")
	trace := []UIEntry{
		entry("loading", "reset", nil),
		entry("live", "snapshot", map[string]string{"g/1": "completed"}),
		entry("live", "delta", map[string]string{"g/1": "completed"}),
	}
	if got := uiHistoryOriginRow(historical, firstSight(trace)); got.Verdict != verdictHolds {
		t.Fatalf("history origin = %s, want %s: the snapshot did arrive first", got.Verdict, verdictHolds)
	}
	if got := uiRepublishRow(historical, [][]string{{"g/1"}}); got.Verdict != verdictViolated {
		t.Errorf("republication = %s, want %s", got.Verdict, verdictViolated)
	}
}

// A view that stopped folding deltas would satisfy every rule above, which is
// what the positive control is for.
func TestAViewThatNeverMovesAgainIsCaught(t *testing.T) {
	historical := historyOf("g/1")
	trace := []UIEntry{
		entry("loading", "reset", nil),
		entry("live", "snapshot", map[string]string{"g/1": "completed"}),
	}
	if got := uiLiveRow(historical, firstSight(trace)); got.Verdict != verdictViolated {
		t.Errorf("live control = %s (%s), want %s", got.Verdict, got.After, verdictViolated)
	}
}

// A node the view dropped and drew again, which is a second introduction to
// anything the interface hangs off one.
func TestANodeIntroducedTwiceIsCaught(t *testing.T) {
	trace := []UIEntry{
		entry("live", "snapshot", map[string]string{"g/1": "completed"}),
		entry("live", "delta", map[string]string{}),
		entry("live", "delta", map[string]string{"g/1": "completed"}),
	}
	if got := uiRebirthRow(trace); got.Verdict != verdictViolated {
		t.Errorf("rebirth = %s (%s), want %s", got.Verdict, got.After, verdictViolated)
	}
}

// A session switch is not a rebirth: the view is told to forget, and what comes
// back is the next conversation's first picture.
func TestASwitchIsNotARebirth(t *testing.T) {
	trace := []UIEntry{
		entry("live", "snapshot", map[string]string{"g/1": "completed"}),
		entry("loading", "reset", map[string]string{}),
		entry("live", "snapshot", map[string]string{"g/1": "completed"}),
	}
	if got := uiRebirthRow(trace); got.Verdict != verdictHolds {
		t.Errorf("rebirth across a switch = %s (%s), want %s", got.Verdict, got.After, verdictHolds)
	}
}
