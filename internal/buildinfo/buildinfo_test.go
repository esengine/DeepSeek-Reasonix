package buildinfo

import "testing"

func TestUserAgentDefaultsToDev(t *testing.T) {
	// A fresh process (no SetVersion) reports the dev placeholder.
	if got := Version(); got != devVersion {
		t.Fatalf("Version() = %q, want %q", got, devVersion)
	}
	if got, want := UserAgent(), "Reasonix/dev"; got != want {
		t.Fatalf("UserAgent() = %q, want %q", got, want)
	}
}

func TestSetVersion(t *testing.T) {
	t.Cleanup(func() { SetVersion(devVersion) }) // don't leak into other tests in the package

	SetVersion("v1.2.3")
	if got, want := Version(), "v1.2.3"; got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
	if got, want := UserAgent(), "Reasonix/v1.2.3"; got != want {
		t.Fatalf("UserAgent() = %q, want %q", got, want)
	}

	// An empty/whitespace version is normalized back to the dev placeholder
	// rather than producing a bare "Reasonix/".
	SetVersion("   ")
	if got, want := UserAgent(), "Reasonix/dev"; got != want {
		t.Fatalf("UserAgent() after blank SetVersion = %q, want %q", got, want)
	}
}
