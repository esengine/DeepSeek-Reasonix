package config

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The location a missing-key error names must be the file resolution actually
// reads. Naming the env var instead sent a caller who had exported it hunting
// through a mechanism runtime resolution never consults.
func TestCredentialsLocationForErrorIsWhereAKeyIsRead(t *testing.T) {
	dir := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", dir)

	location := processRoots().credentialsLocationForError()
	if location != UserCredentialsPath() {
		t.Fatalf("location = %q, want %q", location, UserCredentialsPath())
	}
	if err := os.WriteFile(location, []byte("PROBE_API_KEY=written\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, _, ok := storedCredentialValue(processRoots(), "PROBE_API_KEY")
	if !ok || value != "written" {
		t.Fatalf("storedCredentialValue = %q, %v; want the key written to the named location", value, ok)
	}
}

// A key exported into the process environment stays invisible: the fail is
// deliberate, and it is the reason the error has to name a file.
func TestProcessEnvIsNotACredentialSource(t *testing.T) {
	dir := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", dir)
	t.Setenv("PROBE_API_KEY", "exported")

	if _, _, ok := storedCredentialValue(processRoots(), "PROBE_API_KEY"); ok {
		t.Fatal("storedCredentialValue read the process environment")
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("stat .env = %v, want not-exist", err)
	}
}
