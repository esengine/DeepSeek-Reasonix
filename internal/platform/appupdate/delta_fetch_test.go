package appupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/delta"
	"reasonix/internal/platform/update"
)

func fastBackoff(t *testing.T) {
	t.Helper()
	restore := update.Backoff
	update.Backoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { update.Backoff = restore })
}

// chunkStore serves each chunk compressed under its name, failing a chunk for
// as many requests as flaky says before it answers.
func chunkStore(t *testing.T, plains []string, flaky map[string]int) (*httptest.Server, []delta.Chunk) {
	t.Helper()
	stored := map[string][]byte{}
	var chunks []delta.Chunk
	for _, p := range plains {
		h := delta.HashOf([]byte(p))
		stored["/"+delta.ObjectName(h)] = delta.Compress([]byte(p))
		chunks = append(chunks, delta.Chunk{Hash: h, Size: len(p)})
	}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fail := flaky[r.URL.Path] > 0
		if fail {
			flaky[r.URL.Path]--
		}
		mu.Unlock()
		if fail {
			http.Error(w, "gateway", http.StatusBadGateway)
			return
		}
		_, _ = w.Write(stored[r.URL.Path])
	}))
	t.Cleanup(srv.Close)
	return srv, chunks
}

// One chunk that outlasts a whole round of retries gets another round, and
// the chunks that did arrive are not fetched again for it.
func TestAChunkThatFailsARoundGetsAnother(t *testing.T) {
	fastBackoff(t)
	plains := []string{"first chunk", "second chunk", "third chunk"}
	flakyPath := "/" + delta.ObjectName(delta.HashOf([]byte("second chunk")))
	flaky := map[string]int{flakyPath: update.Attempts}
	srv, chunks := chunkStore(t, plains, flaky)
	dir := filepath.Join(testenv.TempDir(t), "chunks")
	tr := update.Transport{Client: srv.Client()}
	if err := fetchChunks(t.Context(), tr, srv.URL, chunks, dir, nil); err != nil {
		t.Fatalf("fetchChunks: %v", err)
	}
	for _, c := range chunks {
		if _, err := os.Stat(filepath.Join(dir, c.Hash)); err != nil {
			t.Fatalf("chunk %s missing: %v", c.Hash, err)
		}
	}
}

func TestAChunkThatNeverArrivesStillFailsTheDelta(t *testing.T) {
	fastBackoff(t)
	flakyPath := "/" + delta.ObjectName(delta.HashOf([]byte("gone")))
	srv, chunks := chunkStore(t, []string{"gone"}, map[string]int{flakyPath: 1 << 20})
	err := fetchChunks(t.Context(), update.Transport{Client: srv.Client()}, srv.URL, chunks, testenv.TempDir(t), nil)
	var status *update.StatusError
	if !errors.As(err, &status) {
		t.Fatalf("err = %v, want the store's refusal", err)
	}
}

// Chunks take the IPv4 route from the second attempt, as a full download does.
func TestChunksFallBackToTheSecondRoute(t *testing.T) {
	fastBackoff(t)
	srv, chunks := chunkStore(t, []string{"only chunk"}, nil)
	broken := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset (ipv6)")
	})}
	tr := update.Transport{Client: broken, Fallback: srv.Client()}
	if err := fetchChunks(t.Context(), tr, srv.URL, chunks, testenv.TempDir(t), nil); err != nil {
		t.Fatalf("fetchChunks: %v", err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Why the delta was abandoned rides on every frame of the full download and
// on the failure that ends it, as the code the abandoning step attached.
func TestTheReasonADeltaWasSkippedReachesTheProgress(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	c := New(Options{Owner: stubOwner{}, Running: "v1.0.0"}).(*capability)
	r := c.report("v2.0.0", DeltaNotSwappable)
	r.Bytes(10, 100)
	if p := c.InstallProgress(); p.DeltaSkipped != DeltaNotSwappable || p.Received != 10 {
		t.Fatalf("progress = %+v", p)
	}
	r.Phase(update.PhaseVerifying)
	if p := c.InstallProgress(); p.DeltaSkipped != DeltaNotSwappable {
		t.Fatalf("verifying frame = %+v", p)
	}
	err := &fullDownloadError{skipped: DeltaFetchFailed, err: errors.Join(update.ErrFetch, errors.New("reset"))}
	p := failed("v2.0.0", err)
	if p.DeltaSkipped != DeltaFetchFailed || p.Code != FailDownload {
		t.Fatalf("failure = %+v, want the download's code and the delta's reason", p)
	}
}

func TestDeltaCodeOfAnUnclassifiedErrorIsGeneric(t *testing.T) {
	if got := deltaCode(errors.New("x")); got != DeltaFailed {
		t.Fatalf("deltaCode = %q", got)
	}
	if got := deltaCode(failAs(DeltaDisk, errors.New("x"))); got != DeltaDisk {
		t.Fatalf("deltaCode = %q", got)
	}
}

// A store that has gone quiet is given up on after a streak of failed chunks,
// long before every chunk has spent its retries, so the full package still
// has its time.
func TestAChunkStoreThatKeepsFailingIsAbandonedEarly(t *testing.T) {
	fastBackoff(t)
	restore := chunkFailureStreak
	chunkFailureStreak = 4
	t.Cleanup(func() { chunkFailureStreak = restore })
	var plains []string
	flaky := map[string]int{}
	for i := range 200 {
		p := fmt.Sprintf("chunk %d", i)
		plains = append(plains, p)
		flaky["/"+delta.ObjectName(delta.HashOf([]byte(p)))] = 1 << 20
	}
	srv, chunks := chunkStore(t, plains, flaky)
	var hits atomic.Int32
	counted := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		return srv.Client().Transport.RoundTrip(r)
	})}
	err := fetchChunks(t.Context(), update.Transport{Client: counted}, srv.URL, chunks, testenv.TempDir(t), nil)
	if !errors.Is(err, errChunksFailing) {
		t.Fatalf("err = %v, want errChunksFailing", err)
	}
	if n := hits.Load(); n > int32(4*deltaParallel*update.Attempts) {
		t.Fatalf("%d requests before giving up, want the streak to stop it early", n)
	}
}

func TestAChunkThatSucceedsResetsTheStreak(t *testing.T) {
	fastBackoff(t)
	restore := chunkFailureStreak
	chunkFailureStreak = 2
	t.Cleanup(func() { chunkFailureStreak = restore })
	plains := []string{"a", "b", "c", "d"}
	flaky := map[string]int{}
	for _, p := range plains {
		flaky["/"+delta.ObjectName(delta.HashOf([]byte(p)))] = 1
	}
	srv, chunks := chunkStore(t, plains, flaky)
	if err := fetchChunks(t.Context(), update.Transport{Client: srv.Client()}, srv.URL, chunks, testenv.TempDir(t), nil); err != nil {
		t.Fatalf("chunks that each fail once then arrive: %v", err)
	}
}

// Running out of the delta's own budget is its own reason, and leaves the
// move's context alive for the full package.
func TestADeltaThatRunsOutOfTimeSaysSo(t *testing.T) {
	restore := deltaBudget
	deltaBudget = 20 * time.Millisecond
	t.Cleanup(func() { deltaBudget = restore })
	_, err := withinDeltaBudget(t.Context(), func(ctx context.Context) (update.TreeHandoff, error) {
		<-ctx.Done()
		return update.TreeHandoff{}, failAs(DeltaFetchFailed, ctx.Err())
	})
	if code := deltaCode(err); code != DeltaTimedOut || !errors.Is(err, errDeltaTooSlow) {
		t.Fatalf("err = %v (%s), want %s", err, code, DeltaTimedOut)
	}
	if t.Context().Err() != nil {
		t.Fatal("the move's own context ended with the delta's")
	}
}

func TestAMoveThatEndsIsNotReportedAsTheDeltaTimingOut(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := withinDeltaBudget(ctx, func(ctx context.Context) (update.TreeHandoff, error) {
		return update.TreeHandoff{}, failAs(DeltaFetchFailed, ctx.Err())
	})
	if code := deltaCode(err); code != DeltaFetchFailed {
		t.Fatalf("code = %s, want the step's own", code)
	}
}
