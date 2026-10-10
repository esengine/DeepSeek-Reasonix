package update

import (
	"runtime"
	"testing"
)

// A release that lacks this client's architecture must resolve to nothing: the
// updater never installs another architecture's package in its place.
func TestAManifestForAnotherArchitectureResolvesNothing(t *testing.T) {
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	m := Manifest{
		Platforms:      map[string]Asset{PlatformKey(runtime.GOOS, other): {URL: "https://example.invalid/other"}},
		NativePackages: map[string]Asset{PlatformKey(runtime.GOOS, other): {URL: "https://example.invalid/other.deb"}},
	}
	if a, ok := m.Asset(); ok {
		t.Fatalf("Asset resolved %+v from a %s-only manifest", a, other)
	}
	if a, ok := m.NativePackage(); ok {
		t.Fatalf("NativePackage resolved %+v from a %s-only manifest", a, other)
	}
}
