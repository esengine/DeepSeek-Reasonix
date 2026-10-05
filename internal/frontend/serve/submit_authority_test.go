package serve

import (
	"net/http"
	"net/http/httptest"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A pane that opens without a transcript mints one on its first turn. The lease
// keeper has never seen that path, and a controller with no write authority
// over its own session silently drops the input — the first message of every
// freshly opened pane came back 409.
func TestSubmitBindsWriteAuthorityToAFreshlyMintedSession(t *testing.T) {
	dir := testenv.TempDir(t)
	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	if ctrl.SessionPath() != "" {
		t.Fatalf("controller starts with session %q, want none", ctrl.SessionPath())
	}
	srv := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := srv.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(operatorHandler(srv))
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/submit", "application/json", strings.NewReader(`{"input":"今日热点"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		t.Fatalf("POST /submit = 409, want the turn admitted on a newly minted session")
	}

	minted := ctrl.SessionPath()
	if minted == "" {
		t.Fatal("submit left the pane without a session file")
	}
	if got, want := leases.HeldPath(), sessionstore.CanonicalSessionPath(minted); got != want {
		t.Fatalf("lease holds %q, want the minted session %q", got, want)
	}
}
