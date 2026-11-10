package modhost

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func needNode(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	return path
}

// fakeHost is the host side of the protocol, driving the real script under a real node.
type fakeHost struct {
	t       *testing.T
	cmd     *exec.Cmd
	in      *json.Encoder
	lines   chan map[string]any
	mu      sync.Mutex
	nextID  int
	waiting map[int]chan map[string]any
	// published collects host/ui/publish params; asked answers host/ui/request.
	published chan map[string]any
	// sessionCalls collects host/session/read and host/prompt/submit params;
	// refuseSubmit makes the host answer a submit with its per-turn refusal.
	sessionCalls chan map[string]any
	refuseSubmit atomic.Bool
	// refuseSubmitLong makes it answer with the byte-ceiling refusal instead.
	refuseSubmitLong atomic.Bool
	// early collects host requests the script sent before extension/initialized,
	// which the protocol forbids and the client answers by killing the process.
	initializedSent atomic.Bool
	early           chan string
	stderr          *safeBuf
}

type safeBuf struct {
	mu sync.Mutex
	b  []byte
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.b = append(s.b, p...)
	s.mu.Unlock()
	return len(p), nil
}
func (s *safeBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return string(s.b) }

// waitStderr returns the script's stderr once it holds want, or what it has after
// ten seconds: the pipe is copied by another goroutine, so a reply on stdout does
// not mean the line written before it has arrived.
func (h *fakeHost) waitStderr(want string) string {
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(h.stderr.String(), want) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	return h.stderr.String()
}

func startHost(t *testing.T, module string, cfg map[string]any) *fakeHost {
	t.Helper()
	node := needNode(t)
	script, err := WriteScript(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	if _, ok := cfg["stateDir"]; !ok {
		cfg["stateDir"] = testenv.TempDir(t)
	}
	raw, _ := json.Marshal(cfg)
	abs, _ := filepath.Abs(module)
	cmd := exec.Command(node, script, abs, filepath.Dir(abs), "demo", string(raw))
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	h := &fakeHost{t: t, cmd: cmd, in: json.NewEncoder(stdin), lines: make(chan map[string]any, 64), waiting: map[int]chan map[string]any{}, published: make(chan map[string]any, 64), sessionCalls: make(chan map[string]any, 64), early: make(chan string, 16), stderr: &safeBuf{}}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("the script wrote a line that is not JSON on the wire: %q", sc.Text())
				continue
			}
			h.dispatch(m)
		}
	}()
	return h
}

func (h *fakeHost) dispatch(m map[string]any) {
	if method, ok := m["method"].(string); ok {
		id := m["id"]
		if !h.initializedSent.Load() && strings.HasPrefix(method, "host/") {
			h.early <- method
		}
		switch method {
		case "host/ui/publish":
			h.published <- m["params"].(map[string]any)
			h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"accepted": true}})
		case "host/session/read":
			h.sessionCalls <- m["params"].(map[string]any)
			if m["params"].(map[string]any)["what"] == "usage" {
				h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"usage": map[string]any{"promptTokens": 120, "completionTokens": 30, "cachedTokens": 100}}})
			} else {
				h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"messages": []any{map[string]any{"role": "user", "text": "hi"}}, "omitted": 2}})
			}
		case "host/prompt/submit":
			h.sessionCalls <- m["params"].(map[string]any)
			result := map[string]any{"queued": true}
			if h.refuseSubmit.Load() {
				result = map[string]any{"queued": false, "refused": "submit_per_turn"}
			}
			if h.refuseSubmitLong.Load() {
				result = map[string]any{"queued": false, "refused": "submit_bytes"}
			}
			h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		case "host/ui/request":
			h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"cancelled": false, "values": map[string]any{"answer": "yes"}}})
		}
		return
	}
	idf, _ := m["id"].(float64)
	h.mu.Lock()
	ch := h.waiting[int(idf)]
	delete(h.waiting, int(idf))
	h.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (h *fakeHost) call(method string, params any, within time.Duration) map[string]any {
	h.t.Helper()
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	ch := make(chan map[string]any, 1)
	h.waiting[id] = ch
	h.mu.Unlock()
	if err := h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		h.t.Fatal(err)
	}
	select {
	case m := <-ch:
		return m
	case <-time.After(within):
		h.t.Fatalf("%s did not answer within %v; stderr:\n%s", method, within, h.stderr.String())
		return nil
	}
}

func (h *fakeHost) initialize() map[string]any {
	h.t.Helper()
	res := h.call("extension/initialize", map[string]any{
		"protocolVersion": "2", "protocolId": "reasonix.extension.v2", "manifest": map[string]any{},
		"session":      map[string]any{"sessionId": "s1", "workspaceRoot": testenv.TempDir(h.t), "generation": 1},
		"capabilities": map[string]any{"contentRefs": true, "uiHost": "desktop", "protocolVersion": "2"},
	}, 20*time.Second)
	if res["error"] == nil {
		h.initializedSent.Store(true)
		h.in.Encode(map[string]any{"jsonrpc": "2.0", "method": "extension/initialized", "params": map[string]any{}})
	}
	return res
}

// intercept sends one event and returns the decision map.
func (h *fakeHost) intercept(event string, payload any) map[string]any {
	h.t.Helper()
	raw, _ := json.Marshal(payload)
	res := h.call("extension/intercept", map[string]any{"event": event, "seq": 1, "payload": json.RawMessage(raw), "timeoutMillis": 5000}, 20*time.Second)
	if res["error"] != nil {
		h.t.Fatalf("intercept %s: %v", event, res["error"])
	}
	return res["result"].(map[string]any)
}

func writeMod(t *testing.T, name, src string) string {
	t.Helper()
	p := filepath.Join(testenv.TempDir(t), name)
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func decision(m map[string]any) string { return fmt.Sprint(m["decision"]) }
