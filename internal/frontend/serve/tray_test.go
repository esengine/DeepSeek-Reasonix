package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/traystate"
)

// stubTray stands in for the window. TrayFold counts its own calls into the
// answer, so an implementation that read it once and cached it cannot pass.
type stubTray struct {
	mu      sync.Mutex
	live    bool
	fold    traystate.State
	reads   int
	applied []TrayPrefs
}

func (s *stubTray) IconLive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live
}

func (s *stubTray) TrayFold() traystate.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	out := s.fold
	out.Panes = s.reads
	return out
}

func (s *stubTray) ApplyTrayPrefs(p TrayPrefs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, p)
}

func trayServer(t *testing.T, host TrayHost) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{Tray: host})
	srv := httptest.NewServer(operatorHandler(h))
	t.Cleanup(srv.Close)
	return srv
}

func trayGet[T any](t *testing.T, srv *httptest.Server, path string) T {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func trayPut(t *testing.T, srv *httptest.Server, body string) (int, TrayPrefs) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/tray/prefs", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out TrayPrefs
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// A kernel with no window has no icon to answer for, and says so by not being
// there — which is the null a browser tab has always rendered.
func TestATraySurfaceExistsOnlyWhereAWindowDoes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	for _, path := range []string{"/tray/prefs", "/tray/state"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s answered 200 with no window behind it", path)
		}
	}
}

// What is written is what is read back, and it is the config file that holds
// it: a shell keeping its own copy would give two shells two answers.
func TestTrayPrefsRoundTripThroughTheConfigFile(t *testing.T) {
	host := &stubTray{live: true}
	srv := trayServer(t, host)

	status, got := trayPut(t, srv, `{"icon":true,"closeToTray":true}`)
	if status != http.StatusOK {
		t.Fatalf("PUT = %d, want 200", status)
	}
	if !got.Icon || !got.CloseToTray || !got.Live {
		t.Fatalf("PUT answered %+v, want all three on", got)
	}
	if read := trayGet[TrayPrefs](t, srv, "/tray/prefs"); read != got {
		t.Errorf("GET = %+v, want the answer PUT gave: %+v", read, got)
	}
	// The durable half really is durable: a reader that never saw the request
	// finds the same thing.
	if cfg := config.LoadForEdit(config.UserConfigPath()); cfg.DesktopTray() == "off" || !cfg.DesktopClosesToBackground() {
		t.Error("the config file does not hold what the endpoint answered with")
	}
	if len(host.applied) != 1 || !host.applied[0].CloseToTray {
		t.Errorf("the window was told %+v, want the close behaviour it must act on now", host.applied)
	}
}

// The one state this must never produce is a hidden window with nothing left to
// bring it back, so backgrounding depends on an icon being asked for and up.
func TestBackgroundingIsRefusedWithoutAnIconToComeBackTo(t *testing.T) {
	t.Run("no icon asked for", func(t *testing.T) {
		srv := trayServer(t, &stubTray{live: true})
		if _, got := trayPut(t, srv, `{"icon":false,"closeToTray":true}`); got.CloseToTray {
			t.Errorf("prefs = %+v, want backgrounding refused with no icon", got)
		}
	})
	t.Run("icon asked for but this launch has none", func(t *testing.T) {
		srv := trayServer(t, &stubTray{live: false})
		if _, got := trayPut(t, srv, `{"icon":true,"closeToTray":true}`); got.CloseToTray {
			t.Errorf("prefs = %+v, want backgrounding refused with no icon up", got)
		}
	})
}

// The fold is a projection, read afresh every time. A shell that answered from
// a counter of its own would drift from what the panes actually did.
func TestTrayStateIsReadFromTheWindowEveryTime(t *testing.T) {
	host := &stubTray{live: true, fold: traystate.State{Working: 2, Attention: 1}}
	srv := trayServer(t, host)

	first := trayGet[TrayState](t, srv, "/tray/state")
	second := trayGet[TrayState](t, srv, "/tray/state")
	if first.Panes == second.Panes {
		t.Fatal("two reads answered the same fold: the state was cached rather than projected")
	}
	if first.Working != 2 || first.Attention != 1 {
		t.Errorf("state = %+v, want the window's own counts", first)
	}
	// Being needed outranks being busy, and the sentence says so in the
	// language the desktop is set to rather than in the kernel's.
	if first.Mood != "attention" || !first.Busy {
		t.Errorf("state = %+v, want a mood of attention", first)
	}
	if strings.TrimSpace(first.Line) == "" || first.Labels.Quit == "" {
		t.Errorf("state = %+v, want the fold and the menu spelled out", first)
	}
}
