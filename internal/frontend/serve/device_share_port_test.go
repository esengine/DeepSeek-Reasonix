package serve

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func loopbackShare(t *testing.T) *DeviceShare {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	share := NewDeviceShare(fstest.MapFS{"index.html": {Data: []byte("x")}})
	share.addresses = func() []ShareAddress { return []ShareAddress{{Interface: "lo", IP: "127.0.0.1", Kind: AddressLAN}} }
	share.Attach(http.NotFoundHandler())
	t.Cleanup(share.Close)
	return share
}

func TestOpenListensOnTheFixedPort(t *testing.T) {
	share := loopbackShare(t)
	port := freePort(t)
	share.RestorePort(port)
	st, err := share.Open("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://127.0.0.1:" + strconv.Itoa(port); st.Origin != want || st.Port != port {
		t.Fatalf("origin = %q port = %d, want %q and %d", st.Origin, st.Port, want, port)
	}
}

func TestOpenWithoutAPortStillPicksOne(t *testing.T) {
	share := loopbackShare(t)
	st, err := share.Open("127.0.0.1")
	if err != nil || st.Port != 0 || strings.HasSuffix(st.Origin, ":0") {
		t.Fatalf("Open = %+v, %v, want a system-picked port and none configured", st, err)
	}
}

func TestATakenPortIsATypedRefusal(t *testing.T) {
	share := loopbackShare(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port
	share.RestorePort(port)
	if _, err := share.Open("127.0.0.1"); !errors.Is(err, ErrSharePortInUse) {
		t.Fatalf("Open on a taken port = %v, want ErrSharePortInUse", err)
	}
	if share.Status().Open {
		t.Fatal("a failed open left the share open")
	}
}

func TestTheWindowSavesTheSharePortAndReadsItBack(t *testing.T) {
	rig := newShareRig(t)
	port := freePort(t)
	resp := postShareJSON(t, rig.window.URL+"/share/port", `{"port":`+strconv.Itoa(port)+`}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /share/port = %d", resp.StatusCode)
	}
	if got := rig.share.Status().Port; got != port {
		t.Fatalf("status port = %d, want %d", got, port)
	}
	if saved := config.LoadForEdit(config.UserConfigPath()).Serve.SharePort; saved != port {
		t.Fatalf("user config share_port = %d, want %d", saved, port)
	}
	resp = postShareJSON(t, rig.window.URL+"/share/port", `{"port":0}`)
	if resp.StatusCode != http.StatusOK || rig.share.Status().Port != 0 {
		t.Fatalf("clearing = %d port %d, want 200 and unset", resp.StatusCode, rig.share.Status().Port)
	}
}

func TestTheSharePortRouteRefusesWithATypedCode(t *testing.T) {
	rig := newShareRig(t)
	for _, body := range []string{`{"port":80}`, `{"port":70000}`, `{"port":-5}`} {
		resp := postShareJSON(t, rig.window.URL+"/share/port", body)
		if code := deviceRefusal(t, resp); resp.StatusCode != http.StatusBadRequest || code != codeSharePortRange {
			t.Fatalf("POST /share/port %s = %d %q, want 400 %s", body, resp.StatusCode, code, codeSharePortRange)
		}
	}
	if config.LoadForEdit(config.UserConfigPath()).Serve.SharePort != 0 {
		t.Fatal("a refused port reached the user config")
	}
}

func TestTheShareOpenRouteNamesATakenPort(t *testing.T) {
	rig := newShareRig(t)
	rig.share.Close()
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port
	rig.share.RestorePort(port)
	resp := postShareJSON(t, rig.window.URL+"/share/open", `{"ip":"127.0.0.1"}`)
	if code := deviceRefusal(t, resp); resp.StatusCode != http.StatusConflict || code != codeSharePortInUse {
		t.Fatalf("open on a taken port = %d %q, want 409 %s", resp.StatusCode, code, codeSharePortInUse)
	}
}

func postShareJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
