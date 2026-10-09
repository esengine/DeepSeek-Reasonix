package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lanOnly is the address seam a test uses when it needs Open to succeed without
// a real adapter.
func lanOnly(t *testing.T, share *DeviceShare) {
	t.Helper()
	share.addresses = func() []ShareAddress { return []ShareAddress{{Interface: "lo", IP: "127.0.0.1", Kind: AddressLAN}} }
}

// TestShareStateRoundTrips covers the file: what was open reopens, and closing
// records that it is shut.
func TestShareStateRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share-state.json")
	if err := saveShareState(path, persistedShareState{Open: true, Address: "192.168.1.20"}); err != nil {
		t.Fatal(err)
	}
	got := loadShareState(path)
	if !got.Open || got.Address != "192.168.1.20" {
		t.Fatalf("round trip = %+v, want open on 192.168.1.20", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

// TestMissingShareStateReadsAsClosed covers the first run.
func TestMissingShareStateReadsAsClosed(t *testing.T) {
	if got := loadShareState(filepath.Join(t.TempDir(), "nope", "share-state.json")); got.Open {
		t.Fatalf("a missing file read as open: %+v", got)
	}
	if got := loadShareState(""); got.Open {
		t.Fatalf("an empty path read as open: %+v", got)
	}
}

// TestOpeningTheShareRecordsItsAddress is the write half: the address is kept
// only after the listener is real.
func TestOpeningTheShareRecordsItsAddress(t *testing.T) {
	var saved []persistedShareState
	share := NewDeviceShare(nil)
	lanOnly(t, share)
	share.Attach(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	share.persistState = func(st persistedShareState) error { saved = append(saved, st); return nil }
	t.Cleanup(share.Close)

	if _, err := share.Open("127.0.0.1"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(saved) == 0 || !saved[len(saved)-1].Open || saved[len(saved)-1].Address != "127.0.0.1" {
		t.Fatalf("open recorded %+v, want open on 127.0.0.1", saved)
	}
}

// TestClosingTheShareRecordsItShut is the other half of that write.
func TestClosingTheShareRecordsItShut(t *testing.T) {
	var saved []persistedShareState
	share := NewDeviceShare(nil)
	lanOnly(t, share)
	share.Attach(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	share.persistState = func(st persistedShareState) error { saved = append(saved, st); return nil }

	if _, err := share.Open("127.0.0.1"); err != nil {
		t.Fatalf("open: %v", err)
	}
	share.Close()
	last := saved[len(saved)-1]
	if last.Open || last.Address != "" {
		t.Fatalf("close recorded %+v, want shut with no address", last)
	}
}

// TestReopenUsesTheRememberedAddress is the restart path: the share comes back
// where it was, which is what keeps a paired phone's cookie valid.
func TestReopenUsesTheRememberedAddress(t *testing.T) {
	share := NewDeviceShare(nil)
	share.Attach(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// The remembered address comes first in what the share offers, so opening
	// somewhere else would prove the memory was ignored.
	share.addresses = func() []ShareAddress {
		return []ShareAddress{{IP: "127.0.0.2", Kind: AddressLAN}, {IP: "127.0.0.1", Kind: AddressLAN}}
	}
	t.Cleanup(share.Close)

	st, err := share.Reopen("127.0.0.1")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !st.Open || !strings.HasPrefix(st.Origin, "http://127.0.0.1:") {
		t.Fatalf("reopened on %q (open=%v), want the remembered 127.0.0.1", st.Origin, st.Open)
	}
}

// TestReopenFallsBackWhenTheAddressIsGone covers a network change: the old
// address is no longer offered, so the share opens on the first one it has.
func TestReopenFallsBackWhenTheAddressIsGone(t *testing.T) {
	share := NewDeviceShare(nil)
	lanOnly(t, share)
	share.Attach(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(share.Close)

	st, err := share.Reopen("10.99.99.99")
	if err != nil {
		t.Fatalf("reopen after a network change: %v", err)
	}
	if !st.Open || st.Origin == "" {
		t.Fatalf("fallback left the share shut: %+v", st)
	}
}
