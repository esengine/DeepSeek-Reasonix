package serve_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/serve"
	"reasonix/internal/tabhost"
	"reasonix/internal/tool"
)

type multiScripted struct {
	mu    sync.Mutex
	turns [][]provider.Chunk
	i     int
}

func (s *multiScripted) Name() string { return "multi-scripted" }
func (s *multiScripted) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan provider.Chunk, 4)
	var chunks []provider.Chunk
	if s.i < len(s.turns) {
		chunks = s.turns[s.i]
		s.i++
	} else {
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}
	}
	go func() {
		defer close(ch)
		for _, c := range chunks {
			ch <- c
		}
	}()
	return ch, nil
}

func multiTextTurn(s string) []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: s}, {Type: provider.ChunkDone}}
}

func testMultiServer(t *testing.T) (*serve.Server, *tabhost.Host, string, string) {
	t.Helper()
	dirA := t.TempDir()
	dirB := t.TempDir()
	build := func(opts tabhost.CreateTabOpts, sink event.Sink) (control.SessionAPI, error) {
		prov := &multiScripted{turns: [][]provider.Chunk{
			multiTextTurn("from-" + filepath.Base(opts.WorkspaceRoot)),
		}}
		ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, sink)
		c := control.New(control.Options{
			Runner:        ag,
			Executor:      ag,
			Sink:          sink,
			Label:         "test",
			SessionDir:    filepath.Join(opts.WorkspaceRoot, "sessions"),
			WorkspaceRoot: opts.WorkspaceRoot,
		})
		c.EnableInteractiveApproval()
		c.EnsureSessionPath()
		return c, nil
	}
	h := tabhost.New(build)
	t.Cleanup(func() {
		h.CloseAll()
		// Allow in-flight turn goroutines to finish snapshot attempts.
		time.Sleep(50 * time.Millisecond)
	})
	a, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dirA, Label: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.CreateTab(tabhost.CreateTabOpts{WorkspaceRoot: dirB, Label: "B"})
	if err != nil {
		t.Fatal(err)
	}
	ctrl, _, err := h.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	bc := serve.NewBroadcaster()
	srv := serve.New(ctrl.(*control.Controller), bc, config.ServeConfig{AuthMode: "none"})
	srv.SetTabHost(h)
	return srv, h, a.ID, b.ID
}

func TestMultiTabListAndSubmit(t *testing.T) {
	srv, _, idA, idB := testMultiServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// List tabs
	res, err := http.Get(ts.URL + "/tabs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var tabs []tabhost.TabMeta
	if err := json.NewDecoder(res.Body).Decode(&tabs); err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 2 {
		t.Fatalf("tabs=%d", len(tabs))
	}

	// Submit to A and B
	for _, id := range []string{idA, idB} {
		body := `{"input":"hello"}`
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/tabs/"+id+"/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("submit %s status=%d", id, resp.StatusCode)
		}
	}
	// Let async turns settle before assertions/cleanup.
	time.Sleep(150 * time.Millisecond)

	// Per-tab history endpoints exist
	for _, id := range []string{idA, idB} {
		r, err := http.Get(ts.URL + "/tabs/" + id + "/history")
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			t.Fatalf("history %s: %d %s", id, r.StatusCode, b)
		}
		r.Body.Close()
	}

	// Activate A
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/tabs/"+idA+"/activate", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("activate status=%d", resp.StatusCode)
	}
}

func TestMultiTabOpenProject(t *testing.T) {
	srv, h, _, _ := testMultiServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	dir := t.TempDir()
	body := `{"workspaceRoot":` + jsonString(dir) + `}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/tabs/open-project", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var meta tabhost.TabMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceRoot != dir {
		t.Fatalf("root=%q", meta.WorkspaceRoot)
	}
	if len(h.ListTabs()) != 3 {
		t.Fatalf("want 3 tabs got %d", len(h.ListTabs()))
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
