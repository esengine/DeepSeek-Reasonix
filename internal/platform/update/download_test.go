package update

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aead.dev/minisign"

	"reasonix/internal/base/tempdir"
)

// release is one published version as the catalog and its manifest describe it.
type release struct {
	version  string
	artifact []byte
}

// releaseServer serves a catalog, a per-version immutable manifest, and the
// signed artifacts — the same three hops a real rollback walks.
type releaseServer struct {
	*httptest.Server
	hits atomic.Int32
}

func serveReleases(t *testing.T, releases ...release) *releaseServer {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	restore := verifyArtifact
	verifyArtifact = func(data, sig []byte) error { return verifyWith(string(pubText), data, sig) }
	t.Cleanup(func() { verifyArtifact = restore })

	rs := &releaseServer{}
	mux := http.NewServeMux()
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(rs.Close)

	idx := Index{SchemaVersion: 1}
	for _, rel := range releases {
		name := "Reasonix-" + CurrentPlatform() + ".tar.gz"
		base := rs.URL + "/" + rel.version + "/"
		idx.Versions = append(idx.Versions, IndexEntry{
			Version:  rel.version,
			Tag:      "studio-" + rel.version,
			Manifest: base + "latest.json",
		})
		m := Manifest{
			Version: rel.version,
			Platforms: map[string]Asset{CurrentPlatform(): {
				URL:    base + name,
				Sig:    base + name + ".minisig",
				Size:   int64(len(rel.artifact)),
				SHA256: sha256Hex(rel.artifact),
			}},
		}
		mux.HandleFunc("/"+rel.version+"/latest.json", serveJSON(t, m))
		mux.HandleFunc("/"+rel.version+"/"+name, serveBytes(rel.artifact))
		mux.HandleFunc("/"+rel.version+"/"+name+".minisig", serveBytes(minisign.Sign(priv, rel.artifact)))
	}
	mux.HandleFunc("/versions.json", serveJSON(t, idx))
	return rs
}

func serveJSON(t *testing.T, v any) http.HandlerFunc {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return serveBytes(body)
}

// serveBytes honours Range, as the mirror and GitHub both do.
func serveBytes(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}
}

func (rs *releaseServer) updater(t *testing.T, current string) *Updater {
	t.Helper()
	return New(Options{
		Current:  current,
		IndexURL: rs.URL + "/versions.json",
		HTTP:     rs.Client(),
		CacheDir: tempdir.New(t),
	})
}

// The point of the catalog: the version behind the running one downloads and
// verifies through exactly the path the newest one does.
func TestDownloadRollsBackToAnOlderVersion(t *testing.T) {
	old, latest := []byte("the v1.0.0 build"), []byte("the v2.0.0 build")
	rs := serveReleases(t, release{"v1.0.0", old}, release{"v2.0.0", latest})
	u := rs.updater(t, "v2.0.0")

	got, err := u.Download(context.Background(), "v1.0.0", Report{})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got.Version != "v1.0.0" {
		t.Fatalf("downloaded %s, want the version that was asked for", got.Version)
	}
	cached, body, err := Cache{Dir: u.opts.CacheDir}.Verified("v1.0.0")
	if err != nil {
		t.Fatalf("the rollback artifact should be cached ready to install: %v", err)
	}
	if string(body) != string(old) {
		t.Fatalf("cached the wrong build: %q", body)
	}
	if cached.Kind != KindTarball {
		t.Fatalf("cached kind = %q, want %q", cached.Kind, KindTarball)
	}
}

// The pause between the last byte and a usable cache is signature work. A UI
// that is never told about it shows a full bar and no explanation, so the
// phases have to arrive in order and verifying has to be among them.
func TestDownloadNarratesItsPhases(t *testing.T) {
	rs := serveReleases(t, release{"v2.0.0", []byte(strings.Repeat("x", 4096))})
	u := rs.updater(t, "v1.0.0")

	var last int64
	var phases []string
	report := Report{
		Bytes: func(received, _ int64) { last = received },
		Phase: func(p string) { phases = append(phases, p) },
	}
	if _, err := u.Download(context.Background(), "v2.0.0", report); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if last != 4096 {
		t.Fatalf("final progress = %d bytes, want the whole 4096", last)
	}
	want := []string{PhaseDownloading, PhaseVerifying, PhaseCached}
	if strings.Join(phases, ">") != strings.Join(want, ">") {
		t.Fatalf("phases = %v, want %v", phases, want)
	}

	// A cache hit still ends at PhaseCached; a UI must not be left mid-download.
	phases = nil
	if _, err := u.Download(context.Background(), "v2.0.0", report); err != nil {
		t.Fatalf("second Download: %v", err)
	}
	if strings.Join(phases, ">") != PhaseCached {
		t.Fatalf("cache-hit phases = %v, want just %q", phases, PhaseCached)
	}
}

// A second Download must not refetch what is already verified on disk: the user
// who clicked download and then install pays for one transfer, not two.
func TestDownloadReusesTheCachedArtifact(t *testing.T) {
	rs := serveReleases(t, release{"v2.0.0", []byte("artifact")})
	u := rs.updater(t, "v1.0.0")

	if _, err := u.Download(context.Background(), "v2.0.0", Report{}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	after := rs.hits.Load()
	if _, err := u.Download(context.Background(), "v2.0.0", Report{}); err != nil {
		t.Fatalf("second Download: %v", err)
	}
	// The catalog and manifest are still read (the entry may have been
	// republished); the artifact and its signature must not be.
	if fetched := rs.hits.Load() - after; fetched > 2 {
		t.Fatalf("second Download made %d requests, want the artifact served from cache", fetched)
	}
}

func TestDownloadRefusesAVersionNotInTheCatalog(t *testing.T) {
	rs := serveReleases(t, release{"v2.0.0", []byte("artifact")})
	u := rs.updater(t, "v2.0.0")

	_, err := u.Download(context.Background(), "v1.9.9", Report{})
	if err == nil || !strings.Contains(err.Error(), "not in the published catalog") {
		t.Fatalf("error = %v, want a refusal to install an unpublished version", err)
	}
}

// The catalog row and the manifest it points at must name the same release. A
// row that has drifted would otherwise install a build the user never picked.
func TestDownloadRefusesAManifestForAnotherVersion(t *testing.T) {
	rs := serveReleases(t, release{"v2.0.0", []byte("artifact")})
	u := rs.updater(t, "v1.0.0")
	if _, err := u.ManifestFor(context.Background(), "v2.0.0"); err != nil {
		t.Fatalf("the honest catalog must resolve: %v", err)
	}

	// Repoint the entry at a manifest that names a different version.
	lying := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "versions.json") {
			serveJSON(t, Index{Versions: []IndexEntry{{
				Version: "v2.0.0", Tag: "studio-v2.0.0", Manifest: "http://" + r.Host + "/v2.0.0/latest.json",
			}}})(w, r)
			return
		}
		serveJSON(t, Manifest{Version: "v9.9.9"})(w, r)
	}))
	defer lying.Close()
	u.opts.IndexURL = lying.URL + "/versions.json"

	_, err := u.ManifestFor(context.Background(), "v2.0.0")
	if err == nil || !strings.Contains(err.Error(), "resolves to a manifest for") {
		t.Fatalf("error = %v, want a refusal when the catalog and manifest disagree", err)
	}
}

func TestDownloadRefusesAnUnsignedArtifact(t *testing.T) {
	rs := serveReleases(t, release{"v2.0.0", []byte("artifact")})
	u := rs.updater(t, "v1.0.0")
	verifyArtifact = func([]byte, []byte) error { return fmt.Errorf("signature verification failed") }

	if _, err := u.Download(context.Background(), "v2.0.0", Report{}); err == nil {
		t.Fatal("an artifact whose signature does not check out must not be cached")
	}
	if _, _, err := (Cache{Dir: u.opts.CacheDir}).Verified("v2.0.0"); err == nil {
		t.Fatal("a rejected artifact must leave nothing installable behind")
	}
}

// A deb install must never be handed the portable tarball; when the release
// carries no package, that is a refusal, not a silent substitution.
func TestDownloadRefusesToSubstituteAnArtifactKind(t *testing.T) {
	rs := serveReleases(t, release{"v2.0.0", []byte("artifact")})
	u := rs.updater(t, "v1.0.0")
	u.opts.Kind = KindDeb

	_, err := u.Download(context.Background(), "v2.0.0", Report{})
	if err == nil || !strings.Contains(err.Error(), KindDeb) {
		t.Fatalf("error = %v, want a refusal naming the missing deb", err)
	}
}

// Apply is the whole move; it must not reach the installer for a version that
// failed to download.
func TestApplyInstallsOnlyWhatVerified(t *testing.T) {
	rs := serveReleases(t, release{"v1.0.0", []byte("the v1.0.0 build")})
	u := rs.updater(t, "v2.0.0")
	inst := &recordingInstaller{}

	if err := u.Apply(context.Background(), "v1.0.0", inst, Report{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if inst.got.Version != "v1.0.0" {
		t.Fatalf("installer received %+v, want the requested version", inst.got)
	}
	if err := u.Apply(context.Background(), "v1.2.3", inst, Report{}); err == nil {
		t.Fatal("Apply must fail for a version that is not published")
	}
	if inst.calls != 1 {
		t.Fatalf("installer ran %d times, want only for the verified download", inst.calls)
	}
}

type recordingInstaller struct {
	got   Cached
	calls int
}

func (r *recordingInstaller) Install(_ context.Context, c Cached) error {
	r.got, r.calls = c, r.calls+1
	return nil
}
