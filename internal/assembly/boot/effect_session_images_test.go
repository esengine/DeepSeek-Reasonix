package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// chatWire is the part of a /chat/completions body the fake endpoint reads to
// pick its reply; every captured body is kept verbatim.
type chatWire struct {
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools"`
}

type chatEndpoint struct {
	mu     sync.Mutex
	bodies [][]byte
}

// ServeHTTP asks for a screenshot until the first turn holds two results, then
// answers, streaming the way an OpenAI-compatible endpoint does.
func (e *chatEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req chatWire
	_ = json.Unmarshal(body, &req)
	e.mu.Lock()
	if len(req.Tools) > 0 {
		e.bodies = append(e.bodies, body)
	}
	e.mu.Unlock()
	secondTurn := bytes.Contains(body, []byte("and now?"))
	results := 0
	for _, raw := range req.Messages {
		var m struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(raw, &m)
		if m.Role == "tool" {
			results++
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	delta := `{"content":"done"}`
	finish := "stop"
	if len(req.Tools) > 0 && !secondTurn && results < 2 {
		delta = fmt.Sprintf(`{"tool_calls":[{"index":0,"id":"shot-%d","type":"function","function":{"name":"mcp__screen__shot","arguments":"{}"}}]}`, results+1)
		finish = "tool_calls"
	}
	fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", delta)
	fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"total_tokens\":11}}\n\n", finish)
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func (e *chatEndpoint) captured() [][]byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][]byte(nil), e.bodies...)
}

// runSessionImagesArm takes two screenshots, optionally restarts through the
// session store, and returns the raw body of the next turn's first request.
func runSessionImagesArm(t *testing.T, dir, name string, restart, loseImages bool) (chatWire, string) {
	t.Helper()
	endpoint := &chatEndpoint{}
	chat := httptest.NewServer(endpoint)
	t.Cleanup(chat.Close)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "`+chat.URL+`"
model = "x"
api_key = "test-key"
vision = true
`)
	approveWorkspace(t, dir)
	server := screenshotMCPServer(t)
	t.Cleanup(server.Close)
	build := func() *control.Controller {
		ctrl, err := Build(context.Background(), Options{
			resolvedShell: pinnedEffectShell(),
			Home:          os.Getenv("REASONIX_HOME"),
			WorkspaceRoot: dir,
			Sink:          event.Discard,
			ExtraPlugins:  []plugin.Spec{{Name: "screen", Type: "http", URL: server.URL, Authorized: true}},
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return ctrl
	}
	ctrl := build()
	path := filepath.Join(dir, ".reasonix", "sessions", name+".jsonl")
	ctrl.SetSessionPath(path)
	if err := ctrl.Run(context.Background(), "watch the screen"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if restart {
		ctrl.Close()
		if loseImages {
			loseStoredImages(t, path)
		}
		loaded, err := sessionstore.LoadSession(path)
		if err != nil || loaded == nil {
			t.Fatalf("load %s: %v", path, err)
		}
		ctrl = build()
		if err := ctrl.Resume(loaded, path); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	}
	defer ctrl.Close()
	before := len(endpoint.captured())
	if err := ctrl.Run(context.Background(), "and now?"); err != nil {
		t.Fatalf("Run after restart: %v", err)
	}
	bodies := endpoint.captured()
	if len(bodies) <= before {
		t.Fatal("the second turn sent no request")
	}
	var req chatWire
	if err := json.Unmarshal(bodies[before], &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	var first chatWire
	if err := json.Unmarshal(bodies[0], &first); err != nil {
		t.Fatalf("decode first request body: %v", err)
	}
	if len(first.Messages) == 0 || len(req.Messages) == 0 || !bytes.Equal(first.Messages[0], req.Messages[0]) {
		t.Fatal("the session's system message changed between turns")
	}
	return req, path
}

// Image payloads leave the replayed event log, and the provider still receives
// the resumed history byte for byte as an uninterrupted process sends it, so
// the prefix cache a restart meets is the one it left. The new turn's own user
// message is left out: what rides the turn tail after a restart is owed anew.
func TestEffectRestartedSessionSendsImagesByteIdentical(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("REASONIX_STATE_HOME", "")
	dir := robustTempDir(t)
	t.Chdir(dir)
	uninterrupted, _ := runSessionImagesArm(t, dir, "uninterrupted", false, false)
	restarted, path := runSessionImagesArm(t, dir, "restarted", true, false)
	if len(restarted.Messages) != len(uninterrupted.Messages) || len(restarted.Messages) < 2 {
		t.Fatalf("resumed request has %d messages, uninterrupted %d", len(restarted.Messages), len(uninterrupted.Messages))
	}
	history := len(restarted.Messages) - 1
	images := 0
	for i := range history {
		images += bytes.Count(restarted.Messages[i], []byte(`"type":"image_url"`))
		if !bytes.Equal(restarted.Messages[i], uninterrupted.Messages[i]) {
			t.Fatalf("resumed message %d differs on the wire:\n got %s\nwant %s", i, restarted.Messages[i], uninterrupted.Messages[i])
		}
	}
	if images != 2 {
		t.Fatalf("the resumed history carried %d screenshots on the wire, want 2", images)
	}

	log, err := os.ReadFile(store.SessionEventLog(path))
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	if strings.Contains(string(log), ";base64,") {
		t.Fatal("the event log still carries image payloads inline")
	}
}

// loseStoredImages removes every stored copy of the session's images: the
// blobs and the checkpoint's inline ones.
func loseStoredImages(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(store.SessionBlobsDir(path)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for line := range bytes.Lines(raw) {
		var m provider.Message
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatal(err)
		}
		m.Images = nil
		b, _ := json.Marshal(m)
		out.Write(append(b, '\n'))
	}
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Images with no stored copy left reach the provider as a note on the message
// that held them, and that note lives only in the request: the session keeps
// the lost images as a host record, never as text.
func TestEffectRestartedSessionSaysWhichImagesAreGone(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("REASONIX_STATE_HOME", "")
	dir := robustTempDir(t)
	t.Chdir(dir)
	restarted, path := runSessionImagesArm(t, dir, "lost", true, true)
	note := fmt.Sprintf(provider.UnavailableImagesNote, 1)
	quoted, _ := json.Marshal(note)
	notes, images := 0, 0
	for _, m := range restarted.Messages {
		notes += bytes.Count(m, quoted[1:len(quoted)-1])
		images += bytes.Count(m, []byte(`"type":"image_url"`))
	}
	if notes != 2 || images != 0 {
		t.Fatalf("the resumed request carried %d notes and %d images, want 2 notes and no images", notes, images)
	}
	for _, f := range []string{path, store.SessionEventLog(path)} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("the stored copy is missing")) {
			t.Fatalf("%s stores the rendered note", filepath.Base(f))
		}
	}
}
