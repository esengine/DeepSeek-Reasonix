package update

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/contract/config"
)

// Artifact kinds. Which one the cache holds is part of its identity: a .deb and
// a tarball of the same version install through different paths, so one can
// never stand in for the other.
const (
	KindTarball = "tarball"
	KindDeb     = "deb"
)

// legacyChannel is written into the metadata file and never read back. A build
// from before the version hub rejects metadata without it, so a rollback onto
// one keeps its cache instead of downloading the artifact a second time.
const legacyChannel = "stable"

// NormalizeKind canonicalizes a stored or requested kind. Empty is a portable
// cache written before the field existed, so it reads back as a tarball.
func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindDeb:
		return KindDeb
	case KindTarball, "":
		return KindTarball
	default:
		return kind
	}
}

// Cached is a verified artifact on disk, ready to install. It names the version
// it holds so an installer can refuse one it was not asked for — a rollback
// installs a version that is deliberately not the newest.
type Cached struct {
	Version       string
	Path          string
	Size          int64
	SHA256        string
	Kind          string // KindTarball | KindDeb
	SignaturePath string // required for KindDeb
}

// Cache holds one verified artifact at a time in Dir, beside the metadata that
// says what it is. One slot is deliberate: an update and a rollback both
// download before they install, and the later download supersedes the earlier.
type Cache struct{ Dir string }

// CacheDirFn resolves where downloads are kept. It is a variable so tests can
// redirect the whole updater away from the user's real cache.
var CacheDirFn = defaultCacheDir

// CacheDir is the directory every shell keeps its one pending artifact in,
// created if it is missing. Both shells resolve it the same way so a download
// made by one is the download the other finds.
func CacheDir() (string, error) {
	dir, err := CacheDirFn()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func defaultCacheDir() (string, error) {
	if cd := config.CacheDir(); cd != "" {
		return filepath.Join(cd, "updates"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Reasonix", "updates"), nil
}

// record is the metadata file. It stays separate from Cached because it carries
// what only the file needs: the platform the artifact was fetched for, when, and
// the channel that exists purely for the pre-hub reader described above.
type record struct {
	Version       string `json:"version"`
	Channel       string `json:"channel"`
	Platform      string `json:"platform"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	DownloadedAt  string `json:"downloadedAt"`
	ArtifactKind  string `json:"artifactKind,omitempty"`
	SignaturePath string `json:"signaturePath,omitempty"`
}

// Save verifies data against the asset digest and stores it as the cached
// artifact for version. A deb is refused without its detached signature: the
// system helper re-verifies it at install time and cannot do so from nothing.
func (c Cache) Save(version string, asset Asset, data []byte, kind string, signature []byte) (Cached, error) {
	if err := CheckSHA256(data, asset.SHA256); err != nil {
		return Cached{}, err
	}
	kind = NormalizeKind(kind)
	if kind == KindDeb && len(signature) == 0 {
		return Cached{}, fmt.Errorf("update: deb cache requires a signature")
	}
	rec := record{
		Version:      version,
		Channel:      legacyChannel,
		Platform:     CurrentPlatform(),
		Path:         filepath.Join(c.Dir, artifactName(asset, version)),
		Size:         int64(len(data)),
		SHA256:       asset.SHA256,
		DownloadedAt: time.Now().UTC().Format(time.RFC3339),
		ArtifactKind: kind,
	}
	if err := fileutil.AtomicWriteFile(rec.Path, data, 0o600); err != nil {
		return Cached{}, err
	}
	if kind == KindDeb {
		rec.SignaturePath = rec.Path + ".minisig"
		if err := fileutil.AtomicWriteFile(rec.SignaturePath, signature, 0o600); err != nil {
			return Cached{}, err
		}
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return Cached{}, err
	}
	if err := fileutil.AtomicWriteFile(c.metadataPath(), append(raw, '\n'), 0o600); err != nil {
		return Cached{}, err
	}
	return rec.cached(), nil
}

// Holds reports whether the cache already contains exactly this artifact, so a
// panel can say "downloaded" and an install can skip the network. The bytes on
// disk are re-hashed, not trusted from the metadata.
func (c Cache) Holds(version string, asset Asset, kind string) bool {
	rec, err := c.load()
	if err != nil || !rec.serves(kind) {
		return false
	}
	return rec.Version == version &&
		rec.Platform == CurrentPlatform() &&
		strings.EqualFold(rec.SHA256, asset.SHA256) &&
		rec.Size == asset.Size &&
		fileSHA256Matches(rec.Path, rec.SHA256)
}

// Verified returns the cached artifact and its bytes, but only when it is the
// version asked for. Checking platform and digest without the version would
// hand a rollback whichever release the cache happened to hold.
func (c Cache) Verified(version string) (Cached, []byte, error) {
	rec, err := c.load()
	if err != nil {
		return Cached{}, nil, err
	}
	if rec.Platform != CurrentPlatform() {
		return Cached{}, nil, fmt.Errorf("update: cached update is for %s, current platform is %s", rec.Platform, CurrentPlatform())
	}
	if rec.Version != version {
		return Cached{}, nil, fmt.Errorf("update: cached version %s does not match requested version %s", rec.Version, version)
	}
	data, err := os.ReadFile(rec.Path)
	if err != nil {
		return Cached{}, nil, err
	}
	if err := CheckSHA256(data, rec.SHA256); err != nil {
		return Cached{}, nil, err
	}
	if NormalizeKind(rec.ArtifactKind) == KindDeb {
		if rec.SignaturePath == "" {
			return Cached{}, nil, fmt.Errorf("update: cached deb is missing its signature")
		}
		if _, err := os.Stat(rec.SignaturePath); err != nil {
			return Cached{}, nil, fmt.Errorf("update: cached deb signature is missing")
		}
	}
	return rec.cached(), data, nil
}

func (c Cache) metadataPath() string { return filepath.Join(c.Dir, "downloaded.json") }

func (c Cache) load() (record, error) {
	raw, err := fileencoding.ReadFileUTF8(c.metadataPath())
	if err != nil {
		return record{}, err
	}
	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return record{}, err
	}
	if rec.Version == "" || rec.Platform == "" || rec.Path == "" || rec.SHA256 == "" {
		return record{}, fmt.Errorf("update: cached metadata is incomplete")
	}
	return rec, nil
}

// serves reports whether the record can answer a request for kind. A deb needs
// its signature still on disk; a record written before artifactKind existed is
// a tarball and stays usable as one.
func (r record) serves(kind string) bool {
	stored := NormalizeKind(r.ArtifactKind)
	if NormalizeKind(kind) != KindDeb {
		return stored == KindTarball
	}
	if stored != KindDeb || r.SignaturePath == "" {
		return false
	}
	_, err := os.Stat(r.SignaturePath)
	return err == nil
}

func (r record) cached() Cached {
	return Cached{
		Version:       r.Version,
		Path:          r.Path,
		Size:          r.Size,
		SHA256:        r.SHA256,
		Kind:          NormalizeKind(r.ArtifactKind),
		SignaturePath: r.SignaturePath,
	}
}

// artifactName keeps the published filename so a user who opens the cache
// directory recognizes what is in it; the fallback only runs for a malformed URL.
func artifactName(asset Asset, version string) string {
	if u, err := url.Parse(asset.URL); err == nil {
		if base := filepath.Base(u.Path); base != "." && base != "/" {
			return base
		}
	}
	clean := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(version)
	return "Reasonix-" + clean + "-" + CurrentPlatform() + ".update"
}
