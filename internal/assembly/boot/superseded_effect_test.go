package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// isSummarizerRequest tells the compaction summarizer's own call apart from the
// agent's. Only the agent's requests carry the projection under test.
func isSummarizerRequest(req provider.Request) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part")
}

func countUserTag(req provider.Request, tag string) int {
	n := 0
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			n += strings.Count(m.Content, tag)
		}
	}
	return n
}

// TestEffectFoldDropsSupersededStandingState pins the invariant at the boundary
// that decides it: the host restates standing state on every user turn, and a
// fold must drop the copies it superseded. Without that the count only ever
// grows - retained user turns carry their copy through every later fold - so a
// long session pays for the same block once per turn it ever ran.
func TestEffectFoldDropsSupersededStandingState(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-superseded-effect", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "test-model"
kind = "boot-superseded-effect"
model = "x"
context_window = 32000
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, prompt := range []string{"start", "keep going", "keep going", "keep going",
		"keep going", "keep going", "keep going", "keep going"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}

	var counts []int
	var foldedBefore []int
	for _, req := range rec.requests() {
		if isSummarizerRequest(req) {
			foldedBefore = append(foldedBefore, len(counts))
			continue
		}
		counts = append(counts, countUserTag(req, "<workspace>"))
	}
	if len(foldedBefore) == 0 {
		t.Fatalf("the fixture never compacted; counts=%v", counts)
	}
	if len(counts) < 2 {
		t.Fatalf("too few agent requests to observe a fold: %v", counts)
	}

	// Each fold is checked where it happened, against one more than the request
	// before it: the turn after a fold adds its own copy, so a strict decrease
	// would demand two superseded copies rather than the one this pins.
	dropped := 0
	for _, at := range foldedBefore {
		if at == 0 || at >= len(counts) {
			continue
		}
		dropped++
		if counts[at] > counts[at-1] {
			t.Fatalf("the fold before request %d kept every superseded copy (%d before, %d after): they ride every later fold forever.\ncounts=%v",
				at, counts[at-1], counts[at], counts)
		}
	}
	if dropped == 0 {
		t.Fatalf("no fold landed between two observable requests; the fixture proves nothing.\ncounts=%v", counts)
	}
	// The live turn always restates it, so the model is never left without one.
	if final := counts[len(counts)-1]; final == 0 {
		t.Fatalf("the last request carried no standing state at all; the live turn lost its copy.\ncounts=%v", counts)
	}
}
