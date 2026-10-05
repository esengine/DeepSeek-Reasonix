package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/tempdir"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func assetFor(data []byte, name string) Asset {
	return Asset{
		URL:    "https://dl.reasonix.io/studio-v9.9.9/" + name,
		Size:   int64(len(data)),
		SHA256: sha256Hex(data),
	}
}

func TestCacheRoundTrip(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("verified artifact")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")

	if c.Holds("v9.9.9", asset, KindTarball) {
		t.Fatal("an empty cache should not report a downloaded update")
	}
	meta, err := c.Save("v9.9.9", asset, data, KindTarball, nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Base(meta.Path) != "Reasonix-linux-amd64.tar.gz" {
		t.Fatalf("cached artifact keeps the published name, got %q", meta.Path)
	}
	if meta.Size != int64(len(data)) || meta.Kind != KindTarball {
		t.Fatalf("cached metadata mismatch: %+v", meta)
	}
	if !c.Holds("v9.9.9", asset, KindTarball) {
		t.Fatal("the saved artifact should report as downloaded")
	}
	got, body, err := c.Verified("v9.9.9")
	if err != nil {
		t.Fatalf("Verified: %v", err)
	}
	if string(body) != string(data) || got.Path != meta.Path {
		t.Fatalf("Verified returned %q from %q", body, got.Path)
	}
}

// A rollback deliberately installs a version that is not the newest, so the
// reader must refuse a cache that is intact in every way except the version.
func TestCacheRefusesAnotherVersion(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("verified artifact")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")
	if _, err := c.Save("v9.9.9", asset, data, KindTarball, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, _, err := c.Verified("v9.9.9"); err != nil {
		t.Fatalf("the cached version must still be readable: %v", err)
	}
	if _, _, err := c.Verified("v9.8.0"); err == nil {
		t.Fatal("a cache holding another version must not be installed")
	}
	if c.Holds("v9.8.0", asset, KindTarball) {
		t.Fatal("a cache holding another version must not report as downloaded")
	}
}

func TestCacheRejectsTamperedArtifact(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("verified artifact")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")
	meta, err := c.Save("v9.9.9", asset, data, KindTarball, nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.WriteFile(meta.Path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c.Holds("v9.9.9", asset, KindTarball) {
		t.Fatal("a tampered cached artifact should not match")
	}
	if _, _, err := c.Verified("v9.9.9"); err == nil {
		t.Fatal("a tampered cached artifact must be rejected")
	}
}

func TestCacheRejectsAMismatchedDigestOnSave(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("verified artifact")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")
	asset.SHA256 = sha256Hex([]byte("something else"))
	if _, err := c.Save("v9.9.9", asset, data, KindTarball, nil); err == nil {
		t.Fatal("saving bytes that do not match the manifest digest must fail")
	}
}

func TestDebCacheRequiresSignatureAndRejectsTarballReuse(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("deb-bytes")
	asset := assetFor(data, "Reasonix-linux-amd64.deb")

	if _, err := c.Save("v9.9.9", asset, data, KindDeb, nil); err == nil {
		t.Fatal("deb cache without signature must fail")
	}
	meta, err := c.Save("v9.9.9", asset, data, KindDeb, []byte("minisig-bytes"))
	if err != nil {
		t.Fatalf("Save deb: %v", err)
	}
	if meta.SignaturePath == "" {
		t.Fatal("deb cache must record signature path")
	}
	if !c.Holds("v9.9.9", asset, KindDeb) {
		t.Fatal("deb cache with matching signature should match")
	}
	if c.Holds("v9.9.9", asset, KindTarball) {
		t.Fatal("deb cache must not match tarball requests")
	}
	if err := os.Remove(meta.SignaturePath); err != nil {
		t.Fatal(err)
	}
	if c.Holds("v9.9.9", asset, KindDeb) {
		t.Fatal("deb cache without signature file must not match")
	}
	if _, _, err := c.Verified("v9.9.9"); err == nil {
		t.Fatal("a deb whose signature is gone must not be installed")
	}
}

// Metadata written before artifactKind existed is a portable tarball, and must
// keep working across the update that introduced the field.
func TestLegacyMetadataWithoutKindStaysUsable(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("tarball-bytes")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")
	if _, err := c.Save("v9.9.9", asset, data, KindTarball, nil); err != nil {
		t.Fatal(err)
	}
	rewriteMetadata(t, c, func(raw map[string]any) { delete(raw, "artifactKind") })

	if !c.Holds("v9.9.9", asset, KindTarball) {
		t.Fatal("legacy portable cache should still match tarball")
	}
	if c.Holds("v9.9.9", asset, KindDeb) {
		t.Fatal("legacy portable cache must not match deb")
	}
}

// Channels were retired with canary. Metadata that still names one reads back
// unchanged, and metadata that names a retired one is not rejected for it.
func TestCacheIgnoresTheChannelItWasWrittenWith(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("verified artifact")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")
	if _, err := c.Save("v9.9.9", asset, data, KindTarball, nil); err != nil {
		t.Fatal(err)
	}
	if got := readMetadata(t, c)["channel"]; got != legacyChannel {
		// A pre-hub build rejects metadata whose channel is missing or foreign.
		t.Fatalf("channel = %v, want %q so a rollback onto an older build keeps this cache", got, legacyChannel)
	}
	rewriteMetadata(t, c, func(raw map[string]any) { raw["channel"] = "canary" })
	if _, _, err := c.Verified("v9.9.9"); err != nil {
		t.Fatalf("a retired channel in the metadata must not block the install: %v", err)
	}
}

func TestCacheRejectsAnotherPlatform(t *testing.T) {
	c := Cache{Dir: tempdir.New(t)}
	data := []byte("verified artifact")
	asset := assetFor(data, "Reasonix-linux-amd64.tar.gz")
	if _, err := c.Save("v9.9.9", asset, data, KindTarball, nil); err != nil {
		t.Fatal(err)
	}
	rewriteMetadata(t, c, func(raw map[string]any) { raw["platform"] = "plan9-mips" })

	if _, _, err := c.Verified("v9.9.9"); err == nil {
		t.Fatal("an artifact fetched for another platform must not be installed")
	}
	if c.Holds("v9.9.9", asset, KindTarball) {
		t.Fatal("an artifact fetched for another platform must not report as downloaded")
	}
}

func TestNormalizeKind(t *testing.T) {
	for in, want := range map[string]string{
		"":            KindTarball,
		"tarball":     KindTarball,
		"  TARBALL  ": KindTarball,
		"deb":         KindDeb,
		"DEB":         KindDeb,
	} {
		if got := NormalizeKind(in); got != want {
			t.Errorf("NormalizeKind(%q) = %q, want %q", in, got, want)
		}
	}
	// An unknown kind stays itself so it matches nothing rather than defaulting
	// into a path that would install it.
	if got := NormalizeKind("msix"); got != "msix" {
		t.Errorf("NormalizeKind(msix) = %q, want it preserved", got)
	}
}

func readMetadata(t *testing.T, c Cache) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(c.metadataPath())
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rewriteMetadata(t *testing.T, c Cache, edit func(map[string]any)) {
	t.Helper()
	raw := readMetadata(t, c)
	edit(raw)
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.metadataPath(), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
