package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/releaseasset"
)

// releaseTarGz builds an archive shaped like a published one: the executable
// under a directory, which is where the extractor here looks for it by name.
func releaseTarGz(t *testing.T, executable, content string) []byte {
	t.Helper()
	var gzBuf bytes.Buffer
	zw := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(zw)
	body := []byte(content)
	if err := tw.WriteHeader(&tar.Header{
		Name: "reasonix-linux-amd64/" + executable, Mode: 0o755,
		Size: int64(len(body)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzBuf.Bytes()
}

// servedRelease publishes an archive and returns the download it describes.
func servedRelease(t *testing.T, archive []byte, executable string) releaseasset.CLIDownload {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)
	return releaseasset.CLIDownload{
		URL:        srv.URL + "/reasonix-linux-amd64.tar.gz",
		Asset:      "reasonix-linux-amd64.tar.gz",
		SHA256:     hex.EncodeToString(sum[:]),
		Executable: executable,
	}
}

// runFetch executes the generated script the way a remote's shell would.
func runFetch(t *testing.T, d releaseasset.CLIDownload, dir, bin string) (string, error) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell here to run the remote's own script against")
	}
	cmd := exec.Command(sh, "-c", FetchCommand(d, dir, bin))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The route the auto strategy now takes first, run as the remote would run it.
// String-matching the script proves it was written; running it proves it works.
func TestFetchCommandInstallsAVerifiedRelease(t *testing.T) {
	archive := releaseTarGz(t, "reasonix", "#!/bin/sh\necho reasonix v9.9.9\n")
	d := servedRelease(t, archive, "reasonix")
	root := filepath.ToSlash(testenv.TempDir(t))
	dir := path.Join(root, "bin")
	bin := path.Join(dir, "reasonix")

	if out, err := runFetch(t, d, dir, bin); err != nil {
		t.Fatalf("fetch script failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("the fetched binary is not where the launch will look: %v", err)
	}
	if !strings.Contains(string(got), "reasonix v9.9.9") {
		t.Fatalf("the binary that landed is not the one served: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the scratch directory was left behind: %+v", entries)
	}
}

// The digest is the whole reason this route is safe: it is resolved on the
// side that already trusts its connection, and the archive the remote pulled
// over its own is kept only if it matches.
func TestFetchCommandRefusesAMismatchedDigest(t *testing.T) {
	archive := releaseTarGz(t, "reasonix", "not the release you asked for")
	d := servedRelease(t, archive, "reasonix")
	d.SHA256 = strings.Repeat("0", 64)
	root := filepath.ToSlash(testenv.TempDir(t))
	dir := path.Join(root, "bin")
	bin := path.Join(dir, "reasonix")

	out, err := runFetch(t, d, dir, bin)
	if err == nil {
		t.Fatalf("a mismatched archive was accepted:\n%s", out)
	}
	if _, statErr := os.Stat(bin); statErr == nil {
		t.Fatal("a mismatched archive still left a binary behind")
	}
}

// A URL that answers nothing must fail the route, not leave a truncated file
// that the version probe would then have to reject.
func TestFetchCommandFailsWhenTheReleaseIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	root := filepath.ToSlash(testenv.TempDir(t))
	dir := path.Join(root, "bin")
	bin := path.Join(dir, "reasonix")
	d := releaseasset.CLIDownload{
		URL: srv.URL + "/missing.tar.gz", Asset: "missing.tar.gz",
		SHA256: strings.Repeat("a", 64), Executable: "reasonix",
	}

	if out, err := runFetch(t, d, dir, bin); err == nil {
		t.Fatalf("a 404 was treated as a successful fetch:\n%s", out)
	}
	if _, statErr := os.Stat(bin); statErr == nil {
		t.Fatal("a failed fetch still left a binary behind")
	}
}

// Every operand is interpolated into a shell script, the digest included.
func TestFetchCommandQuotesHostileOperands(t *testing.T) {
	d := releaseasset.CLIDownload{
		URL:        "http://x/'; rm -rf ~; echo '",
		Asset:      "a'; rm -rf ~; echo '.tar.gz",
		SHA256:     strings.Repeat("b", 64),
		Executable: "reasonix",
	}
	cmd := FetchCommand(d, "/d", "/d/reasonix")
	if strings.Contains(cmd, "; rm -rf ~; echo") && !strings.Contains(cmd, `'\''; rm -rf ~; echo '\''`) {
		t.Fatalf("a hostile operand escaped its quoting:\n%s", cmd)
	}
}

// Stock macOS has curl and no wget; a minimal container often has the reverse,
// and the same split applies to sha256sum against shasum. Guessing either way
// closes the route on half the machines it was written for.
func TestFetchCommandTriesBothToolPairs(t *testing.T) {
	d := releaseasset.CLIDownload{
		URL: "http://x/a.tar.gz", Asset: "a.tar.gz",
		SHA256: strings.Repeat("c", 64), Executable: "reasonix",
	}
	cmd := FetchCommand(d, "/d", "/d/reasonix")
	for _, want := range []string{"command -v curl", "command -v wget", "sha256sum -c -", "shasum -a 256 -c -"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("the script never tries %q:\n%s", want, cmd)
		}
	}
}

// A probe reporting a route the installer never takes is worse than no probe,
// so the two read the same list, in the same order.
func TestProbeReportsTheRoutesTheInstallerTakes(t *testing.T) {
	got := routesFor(Report{NPM: "10.0.0", Downloader: "curl"}, Options{}, "linux", "amd64")
	if len(got) != len(autoRouteOrder) {
		t.Fatalf("auto reported %d routes for the %d the installer takes", len(got), len(autoRouteOrder))
	}
	for i, want := range autoRouteOrder {
		if got[i].Name != want {
			t.Fatalf("route %d is %q, want %q — the probe and the installer disagree", i, got[i].Name, want)
		}
	}
	// Both sides answer for the same names, and neither answers for one the
	// other does not: a name only one of them knows is how a route becomes
	// reachable in a report and nowhere else, or taken and never reported.
	for _, name := range autoRouteOrder {
		if err := routeClosed(name, Report{}, Options{}, "linux", "amd64"); err != nil &&
			strings.Contains(err.Error(), "unknown install route") {
			t.Fatalf("the installer takes %q, which the probe cannot report on", name)
		}
	}
	const bogus = "carrier-pigeon"
	if err := routeClosed(bogus, Report{}, Options{}, "linux", "amd64"); err == nil ||
		!strings.Contains(err.Error(), "unknown install route") {
		t.Fatalf("the probe reported on a route nobody takes: %v", err)
	}
	if _, _, err := runRoute(t.Context(), nil, loginEnv{}, nil, nil, Options{}, bogus, "", "", "", "", nil); err == nil ||
		!strings.Contains(err.Error(), "unknown install route") {
		t.Fatalf("the installer accepted a route nobody reports: %v", err)
	}
}

// A route the caller cannot take is reported closed rather than open: a first
// connect walks these in order, and an open-looking route that fails on every
// machine teaches the reader nothing.
func TestRoutesReportWhatClosesThem(t *testing.T) {
	closed := routesFor(Report{}, Options{}, "linux", "amd64")
	for _, route := range closed {
		if route.OK() {
			t.Fatalf("route %q reads as open on a machine with nothing wired", route.Name)
		}
	}
	if got := routeClosed(routeRemoteFetch, Report{Downloader: ""}, Options{
		ResolveDownload: resolveNothing, ProductVersion: "v2.11.0",
	}, "linux", "amd64"); !errors.Is(got, ErrRemoteFetchUnavailable) {
		t.Fatalf("a machine with no curl or wget reported %v", got)
	}
	if got := routeClosed(routeRemoteFetch, Report{Downloader: "curl"}, Options{
		ResolveDownload: resolveNothing, ProductVersion: "v2.11.0",
	}, "linux", "amd64"); got != nil {
		t.Fatalf("a machine that can fetch reported %v", got)
	}
}

// A build with nothing published to install from is the caller's problem, not
// the machine's, and it is reported ahead of anything about the machine: a
// reader told to install curl would install it and still have no route.
func TestRoutesBlameTheBuildBeforeTheMachine(t *testing.T) {
	for _, name := range []string{routeRemoteFetch, routeDownload} {
		got := routeClosed(name, Report{Downloader: ""}, Options{
			ResolveDownload: resolveNothing, FetchBinary: fetchNothing, ProductVersion: "dev",
		}, "linux", "amd64")
		if !errors.Is(got, ErrNoReleaseForBuild) {
			t.Errorf("%s on a source build reported %v, want the build named", name, got)
		}
	}
}

func fetchNothing(context.Context, string, string, string) ([]byte, error) { return nil, nil }

func resolveNothing(context.Context, string, string, string) (releaseasset.CLIDownload, error) {
	return releaseasset.CLIDownload{}, nil
}

// The machine pulling its own release is tried before anything that spends a
// local uplink, and npm — which needs Node to deliver the same static binary —
// is tried last.
func TestRemoteFetchIsTriedFirstAndNPMLast(t *testing.T) {
	if autoRouteOrder[0] != routeRemoteFetch {
		t.Fatalf("the first route is %q, not the one that costs nobody a transfer", autoRouteOrder[0])
	}
	if last := autoRouteOrder[len(autoRouteOrder)-1]; last != InstallNPM {
		t.Fatalf("the last route is %q; npm needs a Node runtime for the same binary", last)
	}
}
