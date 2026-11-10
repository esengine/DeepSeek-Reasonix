package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionv4/v4fixture"
)

func TestProgressiveTreePublishesLegacyRowsAndFinalCounts(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	rememberWorkspace(root)
	f := v4fixture.New(t)
	f.Root = filepath.Join(filepath.Dir(SessionDirFor(root)), "sessions-v4")
	for i := range 3 {
		dir := f.Session(fmt.Sprintf("desktop-%024x", i+1), 3)
		f.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("a", "user", "find me")})
	}
	h := NewHub(HubOptions{})
	t.Cleanup(h.Shutdown)
	changed := make(chan struct{}, 1)
	stop := h.legacyListings().Subscribe(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		w := httptest.NewRecorder()
		h.tree(w, httptest.NewRequest(http.MethodGet, "/tree?limit=1&progressive=1&q=find", nil))
		var workspaces []treeWorkspace
		if err := json.Unmarshal(w.Body.Bytes(), &workspaces); err != nil {
			t.Fatal(err)
		}
		if len(workspaces) != 1 {
			t.Fatalf("workspaces=%+v", workspaces)
		}
		ws := workspaces[0]
		if ws.CountsComplete == nil {
			t.Fatal("missing coverage")
		}
		if *ws.CountsComplete {
			if ws.Total != 3 || ws.Remaining != 2 || len(ws.Sessions) != 1 || ws.Sessions[0].Title != "find me" {
				t.Fatalf("workspace=%+v", ws)
			}
			if ws.Sessions[0].CountsKnown == nil || !*ws.Sessions[0].CountsKnown {
				t.Fatal("unknown final count")
			}
			break
		}
		select {
		case <-changed:
		case <-deadline.C:
			t.Fatalf("did not settle: %+v", ws)
		}
	}
	w := httptest.NewRecorder()
	h.tree(w, httptest.NewRequest(http.MethodGet, "/tree?limit=1", nil))
	var old []treeWorkspace
	if err := json.Unmarshal(w.Body.Bytes(), &old); err != nil {
		t.Fatal(err)
	}
	if old[0].CountsComplete != nil || old[0].Total != 3 {
		t.Fatalf("legacy contract=%+v", old)
	}
}

func BenchmarkProgressiveTree(b *testing.B) {
	for _, n := range []int{60, 2000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.Setenv("REASONIX_HOME", testenv.TempDir(b))
			root := testenv.TempDir(b)
			rememberWorkspace(root)
			f := v4fixture.New(b)
			f.Root = filepath.Join(filepath.Dir(SessionDirFor(root)), "sessions-v4")
			for i := range n {
				dir := f.Session(fmt.Sprintf("desktop-%024x", i+1), 3)
				f.Batch(dir, v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("a", "user", "history")})
			}
			b.ResetTimer()
			var firstRows, complete time.Duration
			for range b.N {
				start := time.Now()
				h := NewHub(HubOptions{})
				b.Cleanup(h.Shutdown)
				changed := make(chan struct{}, 1)
				stop := h.legacyListings().Subscribe(func() {
					select {
					case changed <- struct{}{}:
					default:
					}
				})
				b.Cleanup(stop)
				w := httptest.NewRecorder()
				h.tree(w, httptest.NewRequest(http.MethodGet, "/tree?progressive=1&limit=50", nil))
				if w.Code != 200 {
					b.Fatalf("status=%d", w.Code)
				}
				b.StopTimer()
				first, done := measureLegacyTreeReady(b, h, w, changed, start)
				firstRows += first
				complete += done
				stop()
				h.Shutdown()
				b.StartTimer()
			}
			b.ReportMetric(float64(firstRows.Microseconds())/1000/float64(b.N), "first-row-ms")
			b.ReportMetric(float64(complete.Microseconds())/1000/float64(b.N), "all-ready-ms")
		})
	}
}

func measureLegacyTreeReady(b *testing.B, h *Hub, response *httptest.ResponseRecorder, changed <-chan struct{}, start time.Time) (time.Duration, time.Duration) {
	b.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	var first time.Duration
	for {
		var rows []treeWorkspace
		if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
			b.Fatalf("rows=%v error=%v", rows, err)
		}
		if first == 0 && len(rows[0].Sessions) > 0 {
			first = time.Since(start)
		}
		if rows[0].CountsComplete != nil && *rows[0].CountsComplete {
			return first, time.Since(start)
		}
		select {
		case <-changed:
		case <-deadline.C:
			b.Fatal("listing did not finish")
		}
		response = httptest.NewRecorder()
		h.tree(response, httptest.NewRequest(http.MethodGet, "/tree?progressive=1&limit=50", nil))
	}
}
