package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/releaseasset"
	"reasonix/internal/testenv"
)

// fakeKernel is a stand-in for the released CLI: it answers the two questions
// the probe asks and nothing else. A real binary would test the archive's size,
// not the script that installs it.
const fakeKernel = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "reasonix v2.11.0"; exit 0; fi
if [ "$1" = "serve" ] && [ "$2" = "--help" ]; then
  echo "  -addr string"
  echo "  -auth string"
  echo "  -token-file string"
  echo "  -port-file string"
  echo "  -pid-file string"
  echo "  -provider-broker reasonix remote"
  echo "  -provider-broker-token-file string"
  exit 0
fi
exit 1
`

// The fetch and probe scripts, actually run against an archive actually
// served. They are shell this package writes and no Go test executed, for a
// line that had never published an archive to fetch — reasoned about, never
// run. A quoting slip, a `find` matching nothing, a digest line the checker
// spells differently: each passes review and fails exactly once.
func TestFetchAndProbeInstallARealArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX scripts need a POSIX shell; the Windows pair is WindowsFetchCommand")
	}
	archive, digest := tarGzKernel(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	home := testenv.TempDir(t)
	bin := uploadedBinPath(home, "reasonix")
	download := releaseasset.CLIDownload{
		URL: server.URL + "/reasonix-linux-amd64.tar.gz", Asset: "reasonix-linux-amd64.tar.gz",
		SHA256: digest, Executable: "reasonix",
	}

	if out, err := runScript(t, FetchCommand(download, dirOf(bin), bin)); err != nil {
		t.Fatalf("the fetch script failed: %v\n%s", err, out)
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("the fetch script reported success and left no binary: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}
	// The staging directory is the archive's, and leaving it behind leaves a
	// copy of the release beside the one that will be run.
	if _, err := os.Stat(filepath.Join(dirOf(bin), ".fetch")); !os.IsNotExist(err) {
		t.Errorf("the fetch staging directory survived: %v", err)
	}

	// The other half: what the probe reads back off what was just installed.
	// This is the claim "the remote runs the version this side asked for",
	// and it is the one nothing had ever checked end to end.
	out, err := runScript(t, LocateCommand(bin, LaunchFlags(true)))
	if err != nil {
		t.Fatalf("the probe script failed: %v\n%s", err, out)
	}
	found := parseCandidates(out)
	var installed *candidate
	for i := range found {
		if found[i].path == bin {
			installed = &found[i]
		}
	}
	if installed == nil {
		t.Fatalf("the probe did not report the binary it had just installed:\n%s", out)
	}
	if installed.version != "2.11.0" {
		t.Errorf("probed version = %q, want the 2.11.0 the archive carries", installed.version)
	}
	if !installed.usable(MinPaneVersion, LaunchFlags(true)) {
		t.Errorf("a freshly installed current kernel read as unusable: %+v", installed)
	}
}

// A digest that does not match must leave nothing behind: a remote that ran a
// binary the checksum rejected is the failure the digest is carried for.
func TestFetchRefusesAnArchiveThatFailsItsDigest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX scripts need a POSIX shell")
	}
	archive, _ := tarGzKernel(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	home := testenv.TempDir(t)
	bin := uploadedBinPath(home, "reasonix")
	download := releaseasset.CLIDownload{
		URL: server.URL + "/reasonix-linux-amd64.tar.gz", Asset: "reasonix-linux-amd64.tar.gz",
		SHA256: hex.EncodeToString(make([]byte, sha256.Size)), Executable: "reasonix",
	}
	if out, err := runScript(t, FetchCommand(download, dirOf(bin), bin)); err == nil {
		t.Fatalf("a mismatched archive was accepted:\n%s", out)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Errorf("a rejected archive still left a binary: %v", err)
	}
	// The archive was downloaded before the digest rejected it, so the staging
	// directory holds a full copy of a release this machine refused. `set -e`
	// skipped the cleanup that followed; only an exit trap runs either way.
	if _, err := os.Stat(filepath.Join(dirOf(bin), ".fetch")); !os.IsNotExist(err) {
		t.Errorf("a rejected archive left its staging directory behind: %v", err)
	}
}

// runScript runs one of this package's remote scripts the way a remote shell
// would. PATH is cut to the system tools the scripts name, so a reasonix on the
// developer's own PATH cannot be mistaken for one on the machine under test.
func runScript(t *testing.T, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tarGzKernel(t *testing.T) (archive []byte, digest string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte(fakeKernel)
	if err := tw.WriteHeader(&tar.Header{
		Name: "reasonix", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), fmt.Sprintf("%x", sum)
}
