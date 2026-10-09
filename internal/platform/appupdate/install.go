package appupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/platform/update"
)

// installTimeout bounds installing a release that is already verified.
const installTimeout = 30 * time.Minute

// prepareTimeout bounds fetching one. A dead connection is caught far sooner by
// the stall timeout, so this only ends a download still making progress, and
// what it received stays on disk for the next attempt to resume.
const prepareTimeout = 2 * time.Hour

// stallTimeout is how long a full download may receive nothing before its
// connection is dropped and the download resumed on a new one.
const stallTimeout = 30 * time.Second

// What a caller tells apart. Each is a different thing to do about it: name a
// version, install this build somewhere the updater recognizes, wait, or start
// the install again.
var (
	ErrNoTarget        = errors.New("appupdate: no version was named")
	ErrUnknownInstall  = errors.New("appupdate: cannot tell where this build is installed")
	ErrInstallInFlight = errors.New("appupdate: an install is already running")
	ErrNothingReady    = errors.New("appupdate: no verified release is waiting for a restart")
)

// installState is the one install this application may have in flight. It is a
// sub-state rather than fields on the capability because its whole content has
// one lifetime: written by the goroutine doing the move, read by every panel
// asking what that move is doing.
type installState struct {
	mu       sync.Mutex
	progress update.Progress
	ready    *readyMove // set only while progress is PhaseReady
}

// readyMove is a verified release and the one act that installs it. Everything
// before that act only reads the install and writes the cache, so a user who
// never allows the restart is left exactly where they were.
type readyMove struct {
	target string
	cache  string
	apply  func(context.Context) error
}

// begin claims the slot, or reports that something else holds it. Claiming and
// checking are one act: two callers that checked first would both start.
func (s *installState) begin(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Running() {
		return false
	}
	s.progress, s.ready = update.Progress{Version: target, Phase: update.PhaseDownloading}, nil
	return true
}

func (s *installState) set(p update.Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress, s.ready = p, nil
}

func (s *installState) park(mv *readyMove) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress, s.ready = update.Progress{Version: mv.target, Phase: update.PhaseReady}, mv
}

// claim takes the waiting release for target, once: a second restart request
// finds nothing ready rather than a second installer.
func (s *installState) claim(target string) (*readyMove, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mv := s.ready
	if mv == nil || !update.SameVersion(mv.target, target) {
		return nil, false
	}
	s.progress, s.ready = update.Progress{Version: target, Phase: update.PhaseApplying}, nil
	return mv, true
}

func (s *installState) read() update.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// InstallProgress answers what the move is doing. It is a projection: a caller
// that missed a frame is restored by this read, and the last thing an install
// does, ending this process, is not a frame anybody receives.
func (c *capability) InstallProgress() update.Progress {
	return c.install.read()
}

// StartInstall begins moving this application to target, forward or back, and
// returns once it is under way. It stops at a verified release waiting for
// CommitInstall: restarting is the user's to allow, because it ends every
// session this application is running.
func (c *capability) StartInstall(install update.Install, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return ErrNoTarget
	}
	if update.SameVersion(target, c.opts.Running) {
		return nil
	}
	if install.Layout.Root == "" {
		return ErrUnknownInstall
	}
	if !c.install.begin(target) {
		return ErrInstallInFlight
	}
	// Detached from the request that asked for it: the caller is answered now,
	// and a download that outlives its HTTP context is the point, not a leak.
	go c.prepare(install, target)
	return nil
}

func (c *capability) prepare(install update.Install, target string) {
	ctx, cancel := context.WithTimeout(context.Background(), prepareTimeout)
	defer cancel()
	mv, err := c.stage(ctx, install, target)
	if err != nil {
		c.install.set(failed(target, err))
		return
	}
	c.install.park(mv)
}

// CommitInstall installs the release waiting for target and hands over to it.
// Like StartInstall it answers once the act is under way: an install that
// works ends this process.
func (c *capability) CommitInstall(target string) error {
	mv, ok := c.install.claim(strings.TrimSpace(target))
	if !ok {
		return ErrNothingReady
	}
	go c.commit(mv)
	return nil
}

func (c *capability) commit(mv *readyMove) {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	restorePin, err := c.pinFor(mv.target)
	if err != nil {
		c.install.set(failed(mv.target, failAs(FailDisk, err)))
		return
	}
	switch err := mv.apply(ctx); {
	case update.DebAuthCancelled(err):
		// A dismissed prompt is a decision, not a failure: the release stays
		// ready, so asking again costs no download.
		restorePin()
		c.install.park(mv)
		return
	case err != nil:
		restorePin()
		c.install.set(failed(mv.target, failAs(FailInstaller, err)))
		return
	}
	if update.CompareVersions(mv.target, c.opts.Running) > 0 {
		_ = recordMove(mv.cache, pendingMove{From: c.opts.Running, To: mv.target})
	}
	c.install.set(update.Progress{Version: mv.target, Phase: update.PhaseRelaunching})
	c.handOver(ctx)
}

// pinFor writes the pin a move leaves behind: going back holds the chosen build,
// so nothing offers the one just left; going forward releases any hold. It is
// written as the install starts, not before the download, and the returned
// func puts the previous pin back when the install does not happen.
func (c *capability) pinFor(target string) (func(), error) {
	prior, next := update.PinnedVersion(), ""
	if update.CompareVersions(target, c.opts.Running) < 0 {
		next = target
	}
	if update.SameVersion(prior, next) {
		return func() {}, nil
	}
	if err := update.Pin(next); err != nil {
		return nil, err
	}
	return func() { _ = update.Pin(prior) }, nil
}

func failed(target string, err error) update.Progress {
	p := update.Progress{Version: target, Phase: update.PhaseFailed, Err: err.Error(), Code: failureCode(err)}
	if full := (*fullDownloadError)(nil); errors.As(err, &full) {
		p.DeltaSkipped = full.skipped
	}
	return p
}

// stage fetches and verifies target and returns the act that installs it.
// Nothing here writes outside the update cache.
func (c *capability) stage(ctx context.Context, install update.Install, target string) (*readyMove, error) {
	dir, err := update.CacheDir()
	if err != nil {
		return nil, failAs(FailDisk, err)
	}
	u, err := c.updater(target, dir, "")
	if err != nil {
		return nil, failAs(FailDownload, err)
	}
	m, err := u.ManifestFor(ctx, target)
	if err != nil {
		return nil, failAs(FailCatalog, err)
	}
	mv := &readyMove{target: target, cache: dir}
	// A dpkg install upgrades through its package, or apt and the filesystem
	// end up disagreeing. It is also the only channel carrying the SPA tree:
	// the versioned layout stages single files.
	if _, ok := m.NativePackage(); ok && c.opts.Line.OwnsInstalledPath(install.Layout.Executable) {
		mv.apply, err = c.stageNativePackage(ctx, dir, target, m)
		return mv, err
	}
	if _, ok := m.Asset(); !ok {
		return nil, failAs(FailNoPackage, fmt.Errorf("appupdate: %s has no installable package for %s; download it from %s", target, update.CurrentPlatform(), m.DownloadPage))
	}
	h, err := c.tryDelta(ctx, install, target, dir, m)
	if err == nil {
		mv.apply = func(context.Context) error {
			self, err := os.Executable()
			if err != nil {
				return err
			}
			return update.StartTreeHandoff(h, self)
		}
		return mv, nil
	}
	skipped := ""
	if !errors.Is(err, errNoDelta) {
		skipped = deltaCode(err)
	}
	cached, err := u.DownloadManifest(ctx, m, c.report(target, skipped))
	if err != nil {
		return nil, &fullDownloadError{skipped: skipped, err: err}
	}
	mv.apply = func(ctx context.Context) error {
		return c.applyDownloaded(ctx, install, target, dir, cached)
	}
	return mv, nil
}

// stageNativePackage verifies a .deb for Polkit. The updater is rebuilt
// declaring the deb kind so it resolves the package rather than the tarball:
// handing a dpkg install the portable archive would leave apt and the
// filesystem disagreeing about what is installed.
func (c *capability) stageNativePackage(ctx context.Context, cacheDir, target string, m *update.Manifest) (func(context.Context) error, error) {
	u, err := c.updater(target, cacheDir, update.KindDeb)
	if err != nil {
		return nil, failAs(FailDownload, err)
	}
	cached, err := u.DownloadManifest(ctx, m, c.report(target, ""))
	if err != nil {
		return nil, err
	}
	if cached.SignaturePath == "" {
		return nil, failAs(FailVerify, fmt.Errorf("appupdate: the package for %s carries no signature", target))
	}
	return func(context.Context) error {
		c.install.set(update.Progress{Version: target, Phase: update.PhaseAuthorizing})
		return c.opts.Line.InstallDeb(cached.Path, cached.SignaturePath, func(phase string) {
			c.install.set(update.Progress{Version: target, Phase: phase})
		})
	}, nil
}

// handOver ends this application so the installed build can take its place. All
// three acts stay the owner's: what releases the application, what starts its
// successor, and what ends it are not one thing on every platform.
func (c *capability) handOver(ctx context.Context) {
	_ = c.opts.Owner.PrepareForUpdate(ctx)
	_ = c.opts.Owner.RelaunchAfterUpdate(ctx)
	c.opts.Owner.EndApplication(ctx)
}

// report narrates a full download; deltaSkipped is why an offered delta was
// not used, carried on every frame so a panel opened mid-download can say so.
func (c *capability) report(target, deltaSkipped string) update.Report {
	var total int64
	return update.Report{
		Bytes: func(received, size int64) {
			total = size
			c.install.set(update.Progress{Version: target, Phase: update.PhaseDownloading, Received: received, Total: size, DeltaSkipped: deltaSkipped})
		},
		Phase: func(phase string) {
			if phase != update.PhaseDownloading {
				c.install.set(update.Progress{Version: target, Phase: phase, Received: total, Total: total, DeltaSkipped: deltaSkipped})
			}
		},
	}
}

// fullDownloadError is a full download that failed after a delta was
// abandoned, so the failure still says why the delta was not used.
type fullDownloadError struct {
	skipped string
	err     error
}

func (e *fullDownloadError) Error() string { return e.err.Error() }
func (e *fullDownloadError) Unwrap() error { return e.err }

// updater names which artifact this install can apply. An empty kind resolves
// the portable asset; KindDeb resolves the package channel.
func (c *capability) updater(target, cacheDir, kind string) (*update.Updater, error) {
	client, err := netclient.NewHTTPClient(update.ProxySpec(), netclient.TransportOptions{})
	if err != nil {
		return nil, err
	}
	// Best-effort IPv4 route: a nil fallback just means retries reuse the first.
	v4, _ := netclient.NewHTTPClient(update.ProxySpec(), netclient.TransportOptions{ForceIPv4: true})
	return update.New(update.Options{
		Current:  c.opts.Running,
		Pinned:   target,
		HTTP:     client,
		Fallback: v4,
		CacheDir: cacheDir,
		IndexURL: update.StudioCatalog,
		Kind:     kind,
		// Go's default user agent is what release-edge bot protection scores
		// worst (#6005), and a 403 there looks like "no versions" to the panel.
		UserAgent:      update.UserAgent(c.opts.Running),
		AttemptTimeout: 5 * time.Second,
		StallTimeout:   stallTimeout,
	}), nil
}
