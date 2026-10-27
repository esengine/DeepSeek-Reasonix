package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestHistoryPagesMatchFullProjectionAndRejectRewrittenCursor(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	session := sessionstore.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "question", CreatedAt: 123})
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "read_file", Arguments: `{"path":"a"}`}}})
	session.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call", Name: "read_file", Content: "answer"})
	ctrl := control.New(control.Options{Executor: agent.New(titleUsageProvider{}, tool.NewRegistry(), session, agent.Options{}, event.Discard)})
	t.Cleanup(ctrl.Close)
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	read := func(query string) (*httptest.ResponseRecorder, historyPageResponse) {
		w := httptest.NewRecorder()
		s.historyPage(w, httptest.NewRequest(http.MethodGet, "/history/page?limit=1"+query, nil))
		var page historyPageResponse
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return w, page
	}
	w, tail := read("")
	if w.Code != http.StatusOK || len(tail.ToolContext) != 1 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	all := append([]historyMessage{}, tail.Messages...)
	cursor := tail.NextCursor
	for cursor != "" {
		w, page := read("&cursor=" + url.QueryEscape(cursor))
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		all = append(page.Messages, all...)
		cursor = page.NextCursor
	}
	if !reflect.DeepEqual(all, s.projectedHistory()) {
		t.Fatalf("paged=%+v full=%+v", all, s.projectedHistory())
	}
	request := httptest.NewRequest(http.MethodGet, "/history/page?limit=1", nil)
	request.Header.Set("If-None-Match", w.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	s.historyPage(cached, request)
	if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
		t.Fatalf("cached=%d %s", cached.Code, cached.Body.String())
	}
	session.Rewrite([]provider.Message{{Role: provider.RoleUser, Content: "replaced"}}, "test")
	stale, _ := read("&cursor=" + url.QueryEscape(tail.NextCursor))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body.String())
	}
}

func TestHistoryPageBoundsAndDeviceReadReach(t *testing.T) {
	session := sessionstore.NewSession("")
	for i := range 1000 {
		session.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprint(i)})
	}
	ctrl := control.New(control.Options{Executor: agent.New(titleUsageProvider{}, tool.NewRegistry(), session, agent.Options{}, event.Discard)})
	t.Cleanup(ctrl.Close)
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	mux := http.NewServeMux()
	s.registerHistoryRoutes(mux)
	for _, device := range []bool{false, true} {
		w := getHistory(mux, "/history/page", device)
		var page historyPageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || len(page.Messages) != 200 || page.Start != 800 || page.Total != 1000 {
			t.Fatalf("status=%d page=%+v", w.Code, page)
		}
	}
	for _, query := range []string{"limit=0", "limit=501", "limit=wrong", "cursor=not-a-cursor", "cursor=" + strings.Repeat("x", 1025)} {
		if w := getHistory(mux, "/history/page?"+query, false); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	if hostOnlyRoute(http.MethodGet, "/history/page") != hostOnlyRoute(http.MethodGet, "/history") {
		t.Fatal("paging changed read authority")
	}
}

func BenchmarkHistoryPageResponse(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		for _, mode := range []string{"full", "page", "not-modified"} {
			b.Run(fmt.Sprintf("%d/%s", n, mode), func(b *testing.B) {
				b.Setenv("REASONIX_HOME", b.TempDir())
				session := sessionstore.NewSession("")
				for i := range n {
					role := provider.RoleUser
					if i%2 == 1 {
						role = provider.RoleAssistant
					}
					session.Add(provider.Message{Role: role, Content: strings.Repeat("x", 1024)})
				}
				ctrl := control.New(control.Options{Executor: agent.New(titleUsageProvider{}, tool.NewRegistry(), session, agent.Options{}, event.Discard)})
				b.Cleanup(ctrl.Close)
				s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
				handler := s.historyPage
				if mode == "full" {
					handler = s.history
				}
				request := httptest.NewRequest(http.MethodGet, "/history/page", nil)
				if mode == "not-modified" {
					w := httptest.NewRecorder()
					handler(w, request)
					request.Header.Set("If-None-Match", w.Header().Get("ETag"))
				}
				b.ResetTimer()
				for range b.N {
					w := httptest.NewRecorder()
					handler(w, request)
					b.ReportMetric(float64(w.Body.Len()), "response-bytes")
					if w.Code != http.StatusOK && w.Code != http.StatusNotModified {
						b.Fatalf("status=%d", w.Code)
					}
				}
			})
		}
	}
}
