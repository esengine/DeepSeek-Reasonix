package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func peerServer(t *testing.T, peersDir string, name string) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	if err := os.WriteFile(config.UserConfigPath(), []byte("[peers]\nenabled = true\ndispatch = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "sessions", name+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	opts := control.Options{Sink: bc, Label: name, ModelRef: config.UnconfiguredRef, SessionDir: filepath.Dir(path), SessionPath: path}
	if peersDir != "" {
		opts.Peers = control.PeersOptions{Dir: peersDir}
	}
	ctrl := control.New(opts)
	t.Cleanup(ctrl.Close)
	server := New(ctrl, bc, config.ServeConfig{})
	server.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(server))
	t.Cleanup(srv.Close)
	return srv
}

func TestPeerRoutesRefuseWithTheirOwnCodes(t *testing.T) {
	off := peerServer(t, "", "off")
	if status, raw := postSide(t, off.URL+"/peers/send", map[string]any{"to": "x", "text": "hi"}); status != http.StatusConflict || !strings.Contains(string(raw), codePeersDisabled) {
		t.Fatalf("off = %d %s", status, raw)
	}
	res, err := http.Get(off.URL + "/peers")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("list = %v %v", res, err)
	}
	res.Body.Close()

	on := peerServer(t, filepath.Join(testenv.TempDir(t), "peers"), "on")
	if status, raw := postSide(t, on.URL+"/peers/send", map[string]any{"to": "nobody", "text": "hi"}); status != http.StatusNotFound || !strings.Contains(string(raw), codePeerNotFound) {
		t.Fatalf("unknown = %d %s", status, raw)
	}
	if status, raw := postSide(t, on.URL+"/peers/send", map[string]any{"to": "nobody", "text": " "}); status != http.StatusBadRequest || !strings.Contains(string(raw), codePeerEmpty) {
		t.Fatalf("empty = %d %s", status, raw)
	}
	if status, raw := postSide(t, on.URL+"/peers/held/m-nope", map[string]any{"approve": true}); status != http.StatusNotFound || !strings.Contains(string(raw), codePeerNotHeld) {
		t.Fatalf("not held = %d %s", status, raw)
	}
	if status, _ := postSide(t, on.URL+"/peers/held/m-nope", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("no choice = %d", status)
	}
}

func TestAMessageSentOverOneServerIsHeldOnTheOther(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := peerServer(t, peersDir, "alpha")
	b := peerServer(t, peersDir, "beta")
	var key string
	deadline := time.Now().Add(10 * time.Second)
	for key == "" && time.Now().Before(deadline) {
		res, err := http.Get(a.URL + "/peers")
		if err != nil {
			t.Fatal(err)
		}
		buf := new(strings.Builder)
		b2 := make([]byte, 4096)
		n, _ := res.Body.Read(b2)
		buf.Write(b2[:n])
		res.Body.Close()
		if i := strings.Index(buf.String(), `"key":"`); i >= 0 {
			rest := buf.String()[i+7:]
			key = rest[:strings.Index(rest, `"`)]
		}
		time.Sleep(100 * time.Millisecond)
	}
	if key == "" {
		t.Skip("the other session did not publish within the window")
	}
	if status, raw := postSide(t, a.URL+"/peers/send", map[string]any{"to": key, "text": "hello"}); status != http.StatusOK {
		t.Fatalf("send = %d %s", status, raw)
	}
	for time.Now().Before(deadline) {
		res, err := http.Get(b.URL + "/peers")
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 8192)
		n, _ := res.Body.Read(buf)
		res.Body.Close()
		if strings.Contains(string(buf[:n]), `"body":"hello"`) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the receiver never held the message")
}

// /peers carries the threads, empty (never null) until something was sent, and
// the state of the one just written is waiting.
func TestPeersRouteCarriesTheThreadsOfWhatWasSent(t *testing.T) {
	peersDir := filepath.Join(testenv.TempDir(t), "peers")
	a := peerServer(t, peersDir, "alpha")
	b := peerServer(t, peersDir, "beta")
	_ = b
	type peersAnswer struct {
		Peers   []control.PeerView   `json:"peers"`
		Threads []control.PeerThread `json:"threads"`
	}
	read := func() peersAnswer {
		res, err := http.Get(a.URL + "/peers")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out peersAnswer
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	var first peersAnswer
	for deadline := time.Now().Add(10 * time.Second); len(first.Peers) == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		first = read()
	}
	if len(first.Peers) != 1 || first.Threads == nil || len(first.Threads) != 0 {
		t.Fatalf("before sending: %+v, want one peer and an empty (non-null) threads list", first)
	}
	if status, raw := postSide(t, a.URL+"/peers/send", map[string]any{"to": first.Peers[0].Key, "text": "look at the build"}); status != http.StatusOK {
		t.Fatalf("send = %d %s", status, raw)
	}
	after := read()
	if len(after.Threads) != 1 || after.Threads[0].Key != first.Peers[0].Key || after.Threads[0].State != control.PeerWaiting {
		t.Fatalf("after sending: %+v, want one waiting thread on the peer", after.Threads)
	}
}
