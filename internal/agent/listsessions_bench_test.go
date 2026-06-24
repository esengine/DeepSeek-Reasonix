package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestListSessionsScalingManual reproduces the sidebar "project list loads
// slowly" report by timing ListSessions against a synthetically large session
// directory. It is gated behind REASONIX_BENCH=1 so it never runs (or writes
// hundreds of MB) during a normal `go test`.
//
//	REASONIX_BENCH=1 go test ./internal/agent -run TestListSessionsScalingManual -v
func TestListSessionsScalingManual(t *testing.T) {
	if os.Getenv("REASONIX_BENCH") != "1" {
		t.Skip("set REASONIX_BENCH=1 to run the scaling benchmark")
	}

	type shape struct {
		sessions int
		turns    int // user/assistant pairs per session
	}
	shapes := []shape{
		{sessions: 100, turns: 20},
		{sessions: 300, turns: 30},
		{sessions: 500, turns: 40},
	}

	for _, s := range shapes {
		name := fmt.Sprintf("%dsessions_%dturns", s.sessions, s.turns)
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			totalBytes := writeSyntheticSessions(t, dir, s.sessions, s.turns)

			// Metadata-only pass: ReadDir + Stat + LoadBranchMeta sidecar.
			start := time.Now()
			order, err := ListSessionOrder(dir)
			if err != nil {
				t.Fatalf("ListSessionOrder: %v", err)
			}
			orderDur := time.Since(start)

			// Cold ListSessions: sidecars have no recorded turn count yet, so this
			// decodes every .jsonl in full (the pre-fix behaviour) and backfills.
			start = time.Now()
			cold, err := ListSessions(dir)
			if err != nil {
				t.Fatalf("ListSessions (cold): %v", err)
			}
			coldDur := time.Since(start)

			// Warm ListSessions: the backfill populated Turns/Preview in the
			// sidecars, so this reads them and skips the whole-file decode — the
			// post-fix steady state the sidebar hits on every refresh.
			start = time.Now()
			warm, err := ListSessions(dir)
			if err != nil {
				t.Fatalf("ListSessions (warm): %v", err)
			}
			warmDur := time.Since(start)

			t.Logf("sessions=%d turns=%d onDisk=%.1fMB | order(meta)=%v (%d) | ListSessions cold(decode+backfill)=%v (%d) | ListSessions warm(sidecar)=%v (%d) | speedup=%.0fx",
				s.sessions, s.turns, float64(totalBytes)/(1<<20),
				orderDur.Round(time.Millisecond), len(order),
				coldDur.Round(time.Millisecond), len(cold),
				warmDur.Round(time.Millisecond), len(warm),
				float64(coldDur)/float64(warmDur))
		})
	}
}

// writeSyntheticSessions writes n session .jsonl files (newline-delimited
// provider.Message, like Session.Save) plus a minimal .meta sidecar each,
// mimicking a heavy thinking-mode user: large assistant Content +
// ReasoningContent. Returns total on-disk session bytes.
func writeSyntheticSessions(t *testing.T, dir string, n, turns int) int64 {
	t.Helper()
	content := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 140)    // ~6 KB
	reasoning := strings.Repeat("Let me think step by step about this problem. ", 220) // ~10 KB
	var total int64
	for i := range n {
		base := fmt.Sprintf("20260101-%06d-deepseek-chat", i)
		path := filepath.Join(dir, base+".jsonl")
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		enc := json.NewEncoder(f)
		for tn := range turns {
			_ = enc.Encode(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("user message %d %s", tn, content[:200])})
			_ = enc.Encode(provider.Message{Role: provider.RoleAssistant, Content: content, ReasoningContent: reasoning})
		}
		if st, err := f.Stat(); err == nil {
			total += st.Size()
		}
		f.Close()

		meta := BranchMeta{
			ID:        base,
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
			UpdatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
			Scope:     "global",
		}
		b, _ := json.Marshal(meta)
		if err := os.WriteFile(path+".meta", b, 0o644); err != nil {
			t.Fatalf("write meta: %v", err)
		}
	}
	return total
}

// TestColdStartProjectTreeManual measures the steady-state cold-start cost that
// remains AFTER removing the desktop project-session-cache.json disk cache: a
// heavy user spread across many workspaces, every session already carrying
// sidecar Turns/Preview. It mirrors ListProjectTree's per-dir fan-out. The point
// is to confirm the disk cache is now redundant — sidecar-only listing of the
// whole tree is already in the low-millisecond range on cold start.
//
//	REASONIX_BENCH=1 go test ./internal/agent -run TestColdStartProjectTreeManual -v
func TestColdStartProjectTreeManual(t *testing.T) {
	if os.Getenv("REASONIX_BENCH") != "1" {
		t.Skip("set REASONIX_BENCH=1 to run the cold-start benchmark")
	}

	const (
		workspaces = 15
		perDir     = 200
		turns      = 25
	)
	root := t.TempDir()
	dirs := make([]string, workspaces)
	var onDisk int64
	for w := range workspaces {
		dir := filepath.Join(root, fmt.Sprintf("ws-%02d", w))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		dirs[w] = dir
		onDisk += writeSyntheticSessions(t, dir, perDir, turns)
		// Warm the sidecars (Turns/Preview), i.e. the steady state a real user is
		// in after the one-time backfill — this is what cold start reads.
		if _, err := ListSessions(dir); err != nil {
			t.Fatalf("warm ListSessions: %v", err)
		}
	}

	// Sequential cold start (sum over dirs).
	start := time.Now()
	seqTotal := 0
	for _, dir := range dirs {
		infos, err := ListSessions(dir)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		seqTotal += len(infos)
	}
	seqDur := time.Since(start)

	// Concurrent fan-out, the way ListProjectTree actually loads dirs.
	start = time.Now()
	var wg sync.WaitGroup
	counts := make([]int, len(dirs))
	for i, dir := range dirs {
		wg.Add(1)
		go func(i int, dir string) {
			defer wg.Done()
			infos, _ := ListSessions(dir)
			counts[i] = len(infos)
		}(i, dir)
	}
	wg.Wait()
	concDur := time.Since(start)

	t.Logf("workspaces=%d sessions/dir=%d totalSessions=%d onDisk=%.0fMB | cold start sequential=%v | cold start concurrent(fan-out)=%v",
		workspaces, perDir, seqTotal, float64(onDisk)/(1<<20),
		seqDur.Round(time.Millisecond), concDur.Round(time.Millisecond))
}
