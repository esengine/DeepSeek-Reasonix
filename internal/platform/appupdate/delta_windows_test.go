//go:build windows

package appupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/update"
)

// readOnlyLikeProgramFiles gives dir the access an all-users install directory
// grants its users: read and execute, nothing that adds or removes an entry.
func readOnlyLikeProgramFiles(t *testing.T, dir string) {
	t.Helper()
	set := func(sddl string) error {
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			return err
		}
		dacl, _, err := sd.DACL()
		if err != nil {
			return err
		}
		return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	}
	if err := set("D:P(A;OICI;GRGX;;;WD)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set("D:P(A;OICI;FA;;;WD)") })
}

// An install the swap could not write into is not staged at all: the swap would
// put every file back and relaunch the old build, so nothing is fetched and the
// full package, whose installer raises its own consent, does the job.
func TestADeltaForAnInstallTheSwapCannotWriteFetchesNothing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	m := &update.Manifest{Deltas: map[string]update.Delta{update.CurrentPlatform(): {
		Index:  update.Asset{URL: srv.URL + "/index.json.zst", Sig: srv.URL + "/index.json.zst.minisig"},
		Chunks: srv.URL,
	}}}
	root, cache := testenv.TempDir(t), testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "app.exe"), []byte("installed"), 0o644); err != nil {
		t.Fatal(err)
	}
	readOnlyLikeProgramFiles(t, root)
	c := New(Options{Owner: stubOwner{}, Running: "v1.0.0", Application: update.Application{PID: 1}}).(*capability)
	install := update.Install{Version: "v1.0.0", Layout: update.Layout{Root: root, Executable: filepath.Join(root, "app.exe")}}
	_, err := c.tryDelta(t.Context(), install, "v2.0.0", cache, m)
	if err == nil {
		t.Fatal("a delta was applied to an install the swap cannot write")
	}
	if code := deltaCode(err); code != DeltaNotSwappable {
		t.Fatalf("abandoned as %q, want %q", code, DeltaNotSwappable)
	}
	if hits != 0 {
		t.Fatalf("%d requests were made for a swap that could only roll back", hits)
	}
}
