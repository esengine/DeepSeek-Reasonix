package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// recordingPlacementProvider logs every request before delegating to the
// shared-window fake, so placement tests can assert provider traffic.
type recordingPlacementProvider struct {
	sharedWindowTestProvider
	requests []provider.Request
}

func (p *recordingPlacementProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	return p.sharedWindowTestProvider.Stream(ctx, req)
}

// TestMaintainForPlacementFoldsAtSoftLine pins the placement check: once a
// tool result pushes the view past the fold trigger, maintenance runs at that
// result's placement — before the next one rides in on a full context.
func TestMaintainForPlacementFoldsAtSoftLine(t *testing.T) {
	prov := &recordingPlacementProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	sess := foldableSessionOverForce(6)
	a := agentOverForceWindow(t, prov, sess, 50_000)
	big := strings.Repeat("word ", 400)
	for a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) < a.compactTrigger()+2_000 {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	before := a.currentProjectionVersion()

	a.maintainForPlacement(context.Background())

	if a.currentProjectionVersion() == before && len(prov.requests) == 0 {
		t.Fatal("past the soft line, placement maintenance did nothing")
	}
	if est, fold := a.estimatedVisibleRequestTokens(a.modelVisibleMessages()), a.compactTrigger(); est >= fold {
		t.Fatalf("view still at or past soft line: %d >= %d", est, fold)
	}
}

// TestMaintainForPlacementNoopBelowSoftLine guards the common case: a healthy
// placement pays only one estimate and never touches the provider.
func TestMaintainForPlacementNoopBelowSoftLine(t *testing.T) {
	prov := &recordingPlacementProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	a := agentOverForceWindow(t, prov, foldableSessionOverForce(6), 50_000)

	a.maintainForPlacement(context.Background())

	if len(prov.requests) != 0 || a.currentProjectionVersion() != 0 {
		t.Fatalf("below-line noop changed state: requests=%d version=%d",
			len(prov.requests), a.currentProjectionVersion())
	}
}

// TestPlacementCheckStrideScalesWithWindow pins the stride invariant: a
// quarter of the soft-to-hard safety band, expressed in bytes, with a floor
// for degenerate tiny windows and a fixed fallback when no window is set.
func TestPlacementCheckStrideScalesWithWindow(t *testing.T) {
	prov := &recordingPlacementProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}

	// 50K window, CompactRatio 0.5: band = (50_000-256) - 25_000 = 24_744 tokens;
	// stride = 24_744/4 = 6_186 tokens × 4 chars/token = 24_744 bytes.
	a := agentOverForceWindow(t, prov, foldableSessionOverForce(2), 50_000)
	if got, want := a.placementCheckStrideBytes(), 24_744; got != want {
		t.Fatalf("50K window stride = %d, want %d", got, want)
	}

	// 5K window: band = (5_000-256) - 2_500 = 2_244 tokens; 2_244/4 = 561 < 2_048 → floor.
	small := agentOverForceWindow(t, prov, foldableSessionOverForce(2), 5_000)
	if got, want := small.placementCheckStrideBytes(), 8*1024; got != want {
		t.Fatalf("5K window stride = %d, want floor %d", got, want)
	}

	// No window configured: fixed fallback.
	nowin := agentOverForceWindow(t, prov, foldableSessionOverForce(2), 0)
	if got, want := nowin.placementCheckStrideBytes(), 64*1024; got != want {
		t.Fatalf("no-window stride = %d, want fallback %d", got, want)
	}
}

// TestPlacementGovernanceLeavesRoomySessionsUntouched pins the zero-impact
// contract: below the soft line every placement pays at most one local
// estimate and the provider is never called; the view is never folded.
func TestPlacementGovernanceLeavesRoomySessionsUntouched(t *testing.T) {
	prov := &recordingPlacementProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	a := agentOverForceWindow(t, prov, foldableSessionOverForce(6), 50_000)

	a.maintainForPlacement(context.Background())
	a.maintainForPlacement(context.Background())

	if prov.calls != 0 || a.currentProjectionVersion() != 0 {
		t.Fatalf("roomy placement checks changed state: calls=%d version=%d",
			prov.calls, a.currentProjectionVersion())
	}
}
