package installlayout

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDecodeCurrentAcceptsSchema1(t *testing.T) {
	raw := []byte(`{
  "schemaVersion": 1,
  "activeVersion": "v1.20.0",
  "activeDir": "versions/v1.20.0"
}
`)
	ptr, err := DecodeCurrent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ptr.ActiveVersion != "v1.20.0" || ptr.ActiveDir != "versions/v1.20.0" {
		t.Fatalf("unexpected pointer: %+v", ptr)
	}
}

func TestDecodeCurrentRejectsUnknownSchemaAndTraversal(t *testing.T) {
	cases := []string{
		`{"schemaVersion":2,"activeVersion":"v1.20.0","activeDir":"versions/v1.20.0"}`,
		`{"schemaVersion":1,"activeVersion":"../evil","activeDir":"versions/v1.20.0"}`,
		`{"schemaVersion":1,"activeVersion":"v1.20.0","activeDir":"/abs/path"}`,
		`{"schemaVersion":1,"activeVersion":"v1.20.0","activeDir":"versions/../etc"}`,
		`{"schemaVersion":1,"activeVersion":"v1.20.0","activeDir":"versions/v1.19.0"}`,
		`{"schemaVersion":1,"activeVersion":"v1.20.0","activeDir":"versions/v1.20.0","extra":1}`,
	}
	for _, raw := range cases {
		if _, err := DecodeCurrent([]byte(raw)); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
	}
}

func TestWriteAndReadCurrentRoundTrip(t *testing.T) {
	root := t.TempDir()
	version := "v1.20.0"
	dir := filepath.Join(root, "versions", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a dummy desktop binary so ValidateActiveDir only needs the dir.
	if err := os.WriteFile(filepath.Join(dir, DesktopBinaryName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	ptr := CurrentPointer{SchemaVersion: 1, ActiveVersion: version, ActiveDir: VersionDirRelative(version)}
	if err := WriteCurrent(root, ptr); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCurrent(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveVersion != version {
		t.Fatalf("got %q", got.ActiveVersion)
	}
}

func TestValidateActiveDirRejectsSymlinkVersionDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege varies on Windows CI")
	}
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "versions", "v1.20.0")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	err := ValidateActiveDir(root, "v1.20.0", "versions/v1.20.0")
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestValidateVersionName(t *testing.T) {
	ok := []string{"v1.20.0", "v1.20.0-preview.1", "v1.19.1"}
	for _, v := range ok {
		if err := ValidateVersionName(v); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	bad := []string{"", "1.20.0", "v1/20", "v1..20", "v1.20.0/../x", `v1.20.0\x`}
	for _, v := range bad {
		if err := ValidateVersionName(v); err == nil {
			t.Fatalf("expected rejection for %q", v)
		}
	}
}
func TestResolveInstallRootFromVersionedDesktop(t *testing.T) {
	root := t.TempDir()
	version := "v1.20.0"
	dir := filepath.Join(root, "versions", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(dir, DesktopBinaryName())
	if err := os.WriteFile(desktop, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCurrent(root, CurrentPointer{SchemaVersion: 1, ActiveVersion: version, ActiveDir: VersionDirRelative(version)}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveInstallRoot(desktop)
	if err != nil || got != root {
		t.Fatalf("got %q err=%v want %q", got, err, root)
	}
}
