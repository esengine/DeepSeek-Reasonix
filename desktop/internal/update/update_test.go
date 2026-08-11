package update

import (
	"crypto/rand"
	"fmt"
	"testing"

	"aead.dev/minisign"
)

// TestEmbeddedPublicKeyParses guards the hard-coded publicKey constant: it must
// parse and carry the expected key ID, so a copy-paste slip is caught here rather
// than silently failing every signature check in the field.
func TestEmbeddedPublicKeyParses(t *testing.T) {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(publicKey)); err != nil {
		t.Fatalf("embedded public key does not parse: %v", err)
	}
	if got := fmt.Sprintf("%016X", key.ID()); got != "AF12CA46F4A9EBB0" {
		t.Fatalf("embedded public key ID = %s, want AF12CA46F4A9EBB0", got)
	}
}

// TestVerifyWith exercises the verify path end-to-end with a throwaway key pair:
// a genuine signature passes, tampered data fails, and a wrong-key signature fails.
func TestVerifyWith(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("the quick brown fox")
	sig := minisign.Sign(priv, data)

	if err := verifyWith(string(pubText), data, sig); err != nil {
		t.Fatalf("genuine signature should verify, got: %v", err)
	}
	if err := verifyWith(string(pubText), []byte("tampered payload"), sig); err == nil {
		t.Fatal("tampered data should fail verification")
	}

	otherPub, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherText, _ := otherPub.MarshalText()
	if err := verifyWith(string(otherText), data, sig); err == nil {
		t.Fatal("signature under a different key should fail verification")
	}
}

// TestPlatformKey pins the key format the manifest generator and the updater both
// rely on; if these drift, lookups silently miss.
func TestPlatformKey(t *testing.T) {
	if got := PlatformKey("darwin", "arm64"); got != "darwin-arm64" {
		t.Fatalf("PlatformKey = %q, want darwin-arm64", got)
	}
}

// TestManifestAsset checks the running-platform lookup returns the listed asset
// and reports absence cleanly.
func TestManifestAsset(t *testing.T) {
	want := Asset{URL: "https://example/app", SHA256: "abc", Size: 42}
	m := Manifest{Platforms: map[string]Asset{CurrentPlatform(): want}}
	got, ok := m.Asset()
	if !ok || got != want {
		t.Fatalf("Asset() = %+v, %v; want %+v, true", got, ok, want)
	}
	if _, ok := (Manifest{Platforms: map[string]Asset{}}).Asset(); ok {
		t.Fatal("Asset() should report absence for an empty manifest")
	}
}

// TestManifestNativePackage covers the optional native_packages field: present
// assets resolve, and older manifests without the field report absence cleanly.
func TestManifestNativePackage(t *testing.T) {
	want := Asset{URL: "https://example/app.deb", SHA256: "def", Size: 99}
	m := Manifest{NativePackages: map[string]Asset{CurrentPlatform(): want}}
	got, ok := m.NativePackage()
	if !ok || got != want {
		t.Fatalf("NativePackage() = %+v, %v; want %+v, true", got, ok, want)
	}
	if _, ok := (Manifest{}).NativePackage(); ok {
		t.Fatal("NativePackage() should report absence when native_packages is nil")
	}
	// Old clients ignore unknown fields; new clients must still read platforms.
	legacy := Manifest{Platforms: map[string]Asset{CurrentPlatform(): {URL: "tar"}}}
	if _, ok := legacy.NativePackage(); ok {
		t.Fatal("legacy manifest without native_packages must not invent one")
	}
	if a, ok := legacy.Asset(); !ok || a.URL != "tar" {
		t.Fatalf("legacy platforms still resolve: %+v %v", a, ok)
	}
}
