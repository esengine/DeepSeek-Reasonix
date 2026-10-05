package appupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/platform/delta"
	"reasonix/internal/platform/update"
)

const (
	deltaParallel  = 8
	deltaRounds    = 2
	maxIndexBytes  = 16 << 20
	maxStoredChunk = 1 << 20
	deltaFetchWait = 30 * time.Second
)

// deltaBudget bounds the whole chunked attempt, and chunkFailureStreak how
// many chunks in a row may fail before it is abandoned: a mirror that has gone
// quiet must leave the full package the time it needs, not spend it first.
var (
	deltaBudget        = 40 * time.Minute
	chunkFailureStreak = 16
)

// errDeltaTooSlow is a chunked attempt that ran out of its budget.
var errDeltaTooSlow = errors.New("appupdate: the chunked update ran out of time")

// errChunksFailing is a chunk store that failed chunkFailureStreak in a row.
var errChunksFailing = errors.New("appupdate: the chunk store kept failing")

// errDeltaMismatch is a published delta that does not belong to the release
// asked for, or whose index is not the one the manifest names.
var errDeltaMismatch = errors.New("appupdate: the published delta does not match the release")

// errNoDelta is a release or an install a delta is not offered to at all,
// which is not a delta abandoned and carries no reason to report.
var errNoDelta = errors.New("appupdate: no chunked update is offered here")

// tryDelta stages target from the chunks this install lacks. A nil error is a
// staged swap; errNoDelta means none was offered; anything else is why the
// offered one was abandoned, carrying its Delta* code. Staging writes only the
// cache and the install's swap backup directory, so the full package can
// always take over. The swap starts once the restart is allowed.
func (c *capability) tryDelta(ctx context.Context, install update.Install, target, cacheDir string, m *update.Manifest) (update.TreeHandoff, error) {
	d, ok := m.Deltas[update.CurrentPlatform()]
	if !ok || !update.TreeHandoffSupported() || install.Layout.Root == "" || c.opts.Application.PID <= 0 {
		return update.TreeHandoff{}, errNoDelta
	}
	if err := update.CheckTreeSwap(install.Layout.Root, update.SwapBackupDir(install.Layout.Root)); err != nil {
		code := DeltaDisk
		if errors.Is(err, update.ErrTreeNotSwappable) {
			code = DeltaNotSwappable
		}
		slog.Warn("appupdate: this install cannot take a chunked update, downloading the full package", "target", target, "code", code, "err", err)
		return update.TreeHandoff{}, failAs(code, err)
	}
	h, err := withinDeltaBudget(ctx, func(ctx context.Context) (update.TreeHandoff, error) {
		return c.stageDelta(ctx, install, target, cacheDir, d)
	})
	if err != nil {
		slog.Warn("appupdate: chunked update unavailable, downloading the full package", "target", target, "code", deltaCode(err), "err", err)
		return update.TreeHandoff{}, err
	}
	h.Outcome = filepath.Join(cacheDir, swapOutcomeName)
	return h, nil
}

func (c *capability) stageDelta(ctx context.Context, install update.Install, target, cacheDir string, d update.Delta) (update.TreeHandoff, error) {
	t, err := c.deltaTransport()
	if err != nil {
		return update.TreeHandoff{}, failAs(DeltaFetchFailed, err)
	}
	x, err := fetchIndex(ctx, t, d.Index, target)
	if err != nil {
		return update.TreeHandoff{}, err
	}
	// Cleared before planning: the backup sits inside the install, and a plan
	// that took chunks from it would lose them when it is cleared.
	backup := filepath.Join(update.SwapBackupDir(install.Layout.Root), x.Version)
	if err := os.RemoveAll(backup); err != nil {
		return update.TreeHandoff{}, failAs(DeltaDisk, err)
	}
	plan, err := delta.PlanFrom(x, install.Layout.Root)
	if err != nil {
		return update.TreeHandoff{}, failAs(DeltaDisk, err)
	}
	work := filepath.Join(cacheDir, "delta")
	chunks := filepath.Join(work, "chunks")
	progress := func(done, total int64) {
		c.install.set(update.Progress{Version: target, Phase: update.PhaseDownloading, Received: done, Total: total})
	}
	if err := fetchChunks(ctx, t, d.Chunks, plan.Missing, chunks, progress); err != nil {
		code := DeltaFetchFailed
		if errors.Is(err, delta.ErrChunkMismatch) {
			code = DeltaMismatch
		}
		return update.TreeHandoff{}, failAs(code, err)
	}
	staging := filepath.Join(work, x.Version, "tree")
	if err := os.RemoveAll(staging); err != nil {
		return update.TreeHandoff{}, failAs(DeltaDisk, err)
	}
	c.install.set(update.Progress{Version: target, Phase: update.PhaseVerifying})
	if err := delta.Assemble(x, plan, install.Layout.Root, chunks, staging); err != nil {
		code := DeltaDisk
		if errors.Is(err, delta.ErrChunkMismatch) || errors.Is(err, delta.ErrFileMismatch) {
			code = DeltaMismatch
		}
		return update.TreeHandoff{}, failAs(code, err)
	}
	_ = os.RemoveAll(chunks)
	h := update.TreeHandoff{
		Version: x.Version, InstallDir: install.Layout.Root, StagingDir: staging, BackupDir: backup,
		Relaunch: install.Layout.Launcher, WaitPIDs: []int{c.opts.Application.PID, os.Getpid()},
	}
	if h.Relaunch == "" {
		h.Relaunch = install.Layout.Executable
	}
	for _, f := range x.Files {
		h.Files = append(h.Files, update.StagedFile{Path: f.Path, SHA256: f.SHA256})
	}
	return h, nil
}

// fetchIndex reads and proves the release's index: the digest the manifest
// names, the release signature, and the version and platform asked for.
func fetchIndex(ctx context.Context, t update.Transport, a update.Asset, target string) (delta.Index, error) {
	packed, err := t.Fetch(ctx, a.URL, maxIndexBytes)
	if err != nil {
		return delta.Index{}, failAs(DeltaFetchFailed, err)
	}
	sig, err := t.Fetch(ctx, a.Sig, update.MaxSignatureSize)
	if err != nil {
		return delta.Index{}, failAs(DeltaFetchFailed, err)
	}
	if s := sha256.Sum256(packed); hex.EncodeToString(s[:]) != a.SHA256 {
		return delta.Index{}, failAs(DeltaMismatch, fmt.Errorf("%w: index digest", errDeltaMismatch))
	}
	if err := update.Verify(packed, sig); err != nil {
		return delta.Index{}, failAs(DeltaMismatch, err)
	}
	x, err := delta.UnpackIndex(packed)
	if err != nil {
		return delta.Index{}, failAs(DeltaMismatch, err)
	}
	if !update.SameVersion(x.Version, target) || x.Platform != update.CurrentPlatform() {
		return delta.Index{}, failAs(DeltaMismatch, fmt.Errorf("%w: index is %s for %s", errDeltaMismatch, x.Version, x.Platform))
	}
	return x, nil
}

// withinDeltaBudget runs stage under deltaBudget. Running out is its own
// reason, told apart from the move's deadline, which ends the full package too.
func withinDeltaBudget(ctx context.Context, stage func(context.Context) (update.TreeHandoff, error)) (update.TreeHandoff, error) {
	dctx, cancel := context.WithTimeoutCause(ctx, deltaBudget, errDeltaTooSlow)
	defer cancel()
	h, err := stage(dctx)
	if err != nil && ctx.Err() == nil && errors.Is(context.Cause(dctx), errDeltaTooSlow) {
		return h, failAs(DeltaTimedOut, fmt.Errorf("%w: %w", errDeltaTooSlow, err))
	}
	return h, err
}

// fetchChunks fetches every missing chunk, giving the ones a round could not
// fetch another round rather than abandoning the rest: what a round fetched is
// cached and not asked for again. A store failing chunkFailureStreak chunks in
// a row is given up on at once.
func fetchChunks(ctx context.Context, t update.Transport, store string, missing []delta.Chunk, dir string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var streak atomic.Int32
	fetch := func(ctx context.Context, hash string) ([]byte, error) {
		b, err := t.Fetch(ctx, store+"/"+delta.ObjectName(hash), maxStoredChunk)
		if err != nil {
			if int(streak.Add(1)) >= chunkFailureStreak {
				cancel(errChunksFailing)
			}
			return nil, err
		}
		streak.Store(0)
		return b, nil
	}
	var err error
	for range deltaRounds {
		if err = delta.Fetch(ctx, missing, fetch, dir, deltaParallel, progress); err == nil || ctx.Err() != nil {
			break
		}
	}
	if err != nil && errors.Is(context.Cause(ctx), errChunksFailing) {
		return fmt.Errorf("%w: %w", errChunksFailing, err)
	}
	return err
}

// deltaTransport reaches the mirror the way the full download does: the
// default route first, the IPv4 one from the second attempt.
func (c *capability) deltaTransport() (update.Transport, error) {
	client, err := netclient.NewHTTPClient(update.ProxySpec(), netclient.TransportOptions{})
	if err != nil {
		return update.Transport{}, err
	}
	// Best-effort IPv4 route: a nil fallback just means retries reuse the first.
	v4, _ := netclient.NewHTTPClient(update.ProxySpec(), netclient.TransportOptions{ForceIPv4: true})
	return update.Transport{
		Client:         client,
		Fallback:       v4,
		UserAgent:      update.UserAgent(c.opts.Running),
		AttemptTimeout: deltaFetchWait,
	}, nil
}

// deltaCode projects the Delta* code the step that gave up attached.
func deltaCode(err error) string {
	var step *stepError
	if errors.As(err, &step) {
		return step.code
	}
	return DeltaFailed
}
