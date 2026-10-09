package update

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Why a download did not produce a verified cache, told apart because each asks
// for something different: a retry, a report, or room on the disk.
var (
	ErrFetch  = errors.New("update: the release could not be downloaded")
	ErrVerify = errors.New("update: the release failed its signature check")
	ErrStore  = errors.New("update: the verified release could not be saved")
)

// MaxSignatureSize caps a detached .minisig. A signature is a few hundred bytes;
// anything near this is a wrong URL, not a signature.
const MaxSignatureSize = int64(64 << 10)

// The steps a download passes through, in order. Verifying is the one worth
// naming: the gap between the last byte and a usable cache is signature and
// digest work, and a UI that cannot say so looks stalled on a large artifact.
const (
	PhaseDownloading = "downloading"
	PhaseVerifying   = "verifying"
	PhaseCached      = "downloaded"
)

// Report is how a download narrates itself to a UI. Both hooks are optional; a
// caller that wants neither passes the zero value.
type Report struct {
	Bytes ProgressFunc       // fires as the artifact arrives
	Phase func(phase string) // fires when the download moves between steps
}

func (r Report) phase(name string) {
	if r.Phase != nil {
		r.Phase(name)
	}
}

// ManifestFor resolves a published version to its own immutable manifest. The
// catalog is the only way in: an entry names that release's <tag>/latest.json,
// which never moves, so an older version resolves exactly as the newest does.
func (u *Updater) ManifestFor(ctx context.Context, version string) (*Manifest, error) {
	idx, err := FetchIndex(ctx, u.opts.HTTP, u.opts.IndexURL, u.opts.UserAgent)
	if err != nil {
		return nil, err
	}
	for _, e := range idx.Versions {
		if !SameVersion(e.Version, version) {
			continue
		}
		m, err := FetchManifestAt(ctx, u.opts.HTTP, e.Manifest, u.opts.UserAgent)
		if err != nil {
			return nil, err
		}
		// The catalog says this tag holds that version; the manifest under it has
		// to agree, or a mis-published row would install a build the user never
		// picked from the list.
		if !SameVersion(m.Version, version) {
			return nil, fmt.Errorf("update: %s resolves to a manifest for %s", version, m.Version)
		}
		return m, nil
	}
	return nil, fmt.Errorf("update: %s is not in the published catalog", version)
}

// Download fetches one published version's artifact for this platform, verifies
// its signature and digest, and caches it ready to install. Any version in the
// catalog is a valid target: a rollback and an update differ only in which one
// is asked for, so both earn every check on this path.
func (u *Updater) Download(ctx context.Context, version string, r Report) (Cached, error) {
	m, err := u.ManifestFor(ctx, version)
	if err != nil {
		return Cached{}, err
	}
	return u.DownloadManifest(ctx, m, r)
}

// DownloadManifest fetches, verifies and caches the artifact a manifest names.
// It is the half the catalog route and a rolling latest.json pointer share: how
// a version is discovered differs between them, what happens to its bytes must
// not — an artifact reached either way earns the same signature and digest.
func (u *Updater) DownloadManifest(ctx context.Context, m *Manifest, r Report) (Cached, error) {
	asset, kind, ok := u.assetIn(m)
	if !ok {
		return Cached{}, fmt.Errorf("update: %s publishes no %s artifact for %s", m.Version, kind, CurrentPlatform())
	}
	cache := Cache{Dir: u.opts.CacheDir}
	if cache.Holds(m.Version, asset, kind) {
		if c, _, err := cache.Verified(m.Version); err == nil {
			r.phase(PhaseCached)
			return c, nil
		}
	}
	t := u.transport()
	r.phase(PhaseDownloading)
	data, err := u.fetchArtifact(ctx, t, asset, r.Bytes)
	if err != nil {
		return Cached{}, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	r.phase(PhaseVerifying)
	sig, err := t.FetchFrom(ctx, asset.SigSources(), MaxSignatureSize)
	if err != nil {
		return Cached{}, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	// Signature first: the digest is only the manifest's claim about the bytes,
	// and the manifest is only trustworthy once its artifact has been verified.
	if err := verifyArtifact(data, sig); err != nil {
		return Cached{}, fmt.Errorf("%w: %w", ErrVerify, err)
	}
	c, err := cache.Save(m.Version, asset, data, kind, sig)
	if err != nil {
		return Cached{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
	r.phase(PhaseCached)
	return c, nil
}

// Apply is the whole move: fetch the version (or reuse what the cache already
// holds), then hand the verified artifact to the host's installer.
func (u *Updater) Apply(ctx context.Context, version string, inst Installer, r Report) error {
	c, err := u.Download(ctx, version, r)
	if err != nil {
		return err
	}
	return inst.Install(ctx, c)
}

// assetIn picks the artifact this install can actually apply. A deb install
// must not be handed the portable tarball, or the next apt operation would find
// a package manager and a filesystem that disagree about what is installed.
func (u *Updater) assetIn(m *Manifest) (Asset, string, bool) {
	if NormalizeKind(u.opts.Kind) == KindDeb {
		a, ok := m.NativePackage()
		return a, KindDeb, ok
	}
	a, ok := m.Asset()
	return a, KindTarball, ok
}

func (u *Updater) transport() Transport {
	return Transport{
		Client:         u.opts.HTTP,
		Fallback:       u.opts.Fallback,
		UserAgent:      u.opts.UserAgent,
		AttemptTimeout: u.opts.AttemptTimeout,
		StallTimeout:   u.opts.StallTimeout,
	}
}

// fetchArtifact downloads through a partial file in the cache, so an attempt
// that times out, or a process that restarts, resumes rather than starting
// over. The partial is named by the digest it must end with, so one left by
// another release is never resumed into this one, and it is removed once
// complete: from then on the cache, not the partial, holds the release.
func (u *Updater) fetchArtifact(ctx context.Context, t Transport, asset Asset, onProgress ProgressFunc) ([]byte, error) {
	if u.opts.CacheDir == "" || !isSHA256Hex(asset.SHA256) {
		return t.DownloadFrom(ctx, asset.Sources(), asset.Size, onProgress)
	}
	if err := os.MkdirAll(u.opts.CacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	name := partialPrefix + strings.ToLower(asset.SHA256) + partialSuffix
	clearPartials(u.opts.CacheDir, name)
	partial := filepath.Join(u.opts.CacheDir, name)
	if err := t.DownloadFile(ctx, asset.Sources(), asset.Size, partial, onProgress); err != nil {
		return nil, err
	}
	defer os.Remove(partial)
	data, err := os.ReadFile(partial)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return data, nil
}

const (
	partialPrefix = "partial-"
	partialSuffix = ".download"
)

// clearPartials removes every partial download but keep: only one release is
// ever being fetched, and an abandoned one is dead weight on the disk.
func clearPartials(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if n != keep && strings.HasPrefix(n, partialPrefix) && strings.HasSuffix(n, partialSuffix) {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
