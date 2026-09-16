package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/servecontract"
	"reasonix/internal/session"
	"reasonix/internal/sessioncontent"
	"reasonix/internal/transcript"
)

func remoteTranscriptFixture(server *httptest.Server) (*App, *remoteTab) {
	tab := &remoteTab{id: "remote", state: "ready", client: server.Client(), base: server.URL, gen: 1,
		routing: remoteTabSessionRouting{currentPath: "/session.jsonl"}}
	return &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}, tab
}

func TestRemoteFollowRequiresV2WithoutLegacyProbe(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/transcript/follow" {
			t.Errorf("unexpected legacy fallback: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(transcript.FollowResponse{ProtocolVersion: 2, Subscription: "sub", Changes: []transcript.Change{}})
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	if _, err := app.RemoteTranscriptFollowForTab(tab.id, transcript.FollowRequest{}); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("old Serve must produce upgrade error: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("unnegotiated server was probed")
	}
	tab.capabilities = map[string]bool{servecontract.TranscriptV2: true}
	response, err := app.RemoteTranscriptFollowForTab(tab.id, transcript.FollowRequest{Subscription: "sub"})
	if err != nil || response.ProtocolVersion != 2 || requests.Load() != 1 {
		t.Fatalf("v2 follow: %+v, %v", response, err)
	}
}

func TestRemoteTranscriptNegotiatesOldServeWithoutMutation(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/transcript/snapshot" || r.URL.Query().Get("session") != "/session.jsonl" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("<html>old Serve index</html>"))
			}))
			defer server.Close()
			app, tab := remoteTranscriptFixture(server)
			result, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{})
			if err != nil || result.Supported || result.Snapshot != nil {
				t.Fatalf("negotiation = %+v, %v", result, err)
			}
			if tab.state != "ready" || tab.gen != 1 {
				t.Fatal("capability probe changed the connection")
			}
		})
	}
}

// A Serve that does not advertise the outline capability must never be probed:
// the client keeps its loaded-turn rail instead of spending a round trip.
func TestRemoteTranscriptOutlineRequiresAdvertisedCapability(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>old Serve index</html>"))
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)

	page, err := app.RemoteTranscriptOutlineForTab(tab.id, transcript.OutlineRequest{SnapshotID: "cut"})
	if err == nil || page.SnapshotID != "" {
		t.Fatalf("unadvertised outline = %+v, %v", page, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("an unadvertised capability issued %d requests", requests.Load())
	}

	// An advertised capability that answers with something other than protocol
	// data is a real error, not a silent downgrade to "unsupported".
	tab.capabilities = map[string]bool{servecontract.TranscriptOutlineV1: true}
	if _, err := app.RemoteTranscriptOutlineForTab(tab.id, transcript.OutlineRequest{SnapshotID: "cut"}); err == nil {
		t.Fatal("an HTML homepage response was accepted as an outline")
	}
	if requests.Load() != 1 {
		t.Fatalf("advertised capability issued %d requests, want 1", requests.Load())
	}
}

func TestRemoteTranscriptOutlineReadsAdvertisedEndpoint(t *testing.T) {
	want := transcript.OutlinePage{
		Boundary: transcript.Boundary{ProtocolVersion: transcript.ProtocolVersion, SnapshotID: "cut"},
		Entries:  []transcript.OutlineEntry{{ID: "m:2", MessageID: "2", Turn: 2, Order: 4, Prompt: "second", Answer: "answer"}},
		Total:    2, NextOffset: 2, Done: true,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transcript/outline" || r.URL.Query().Get("session") != "/session.jsonl" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		var request transcript.OutlineRequest
		if err := json.Unmarshal([]byte(r.URL.Query().Get("request")), &request); err != nil || request.SnapshotID != "cut" {
			t.Errorf("request = %+v, %v", request, err)
		}
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	tab.capabilities = map[string]bool{servecontract.TranscriptOutlineV1: true}

	page, err := app.RemoteTranscriptOutlineForTab(tab.id, transcript.OutlineRequest{SnapshotID: "cut"})
	if err != nil || page.Total != 2 || len(page.Entries) != 1 || page.Entries[0].ID != "m:2" {
		t.Fatalf("outline = %+v, %v", page, err)
	}
}

func TestRemoteTranscriptRejectsLateSessionResponse(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(transcript.Snapshot{Boundary: transcript.Boundary{ProtocolVersion: 1, SnapshotID: "old"}})
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	done := make(chan error, 1)
	go func() { _, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{}); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	app.remoteTabMu.Lock()
	tab.routing.currentPath = "/new-session.jsonl"
	app.remoteTabMu.Unlock()
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "replaced session") {
		t.Fatalf("late response accepted: %v", err)
	}
}

func TestRemoteTabMetadataDoesNotRequestHistory(t *testing.T) {
	var historyReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/history" {
			historyReads.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/status" {
			_, _ = w.Write([]byte(`{"sessionPath":"/session.jsonl","running":false}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	metadata, err := app.RemoteTabMetadata(tab.id)
	if err != nil || historyReads.Load() != 0 || len(metadata.History) != 0 {
		t.Fatalf("metadata requested history: count=%d error=%v", historyReads.Load(), err)
	}
}

func TestRemoteCanonicalSessionHistoryUsesNegotiatedIdentity(t *testing.T) {
	ref := sessioncontent.Ref{Digest: strings.Repeat("a", 64), Bytes: 3, MediaType: "text/plain"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sessionId"); got != "canonical" {
			t.Errorf("sessionId = %q", got)
		}
		switch r.URL.Path {
		case "/session/open":
			_ = json.NewEncoder(w).Encode(session.SessionOpenView{SnapshotSequence: 9, Recent: session.RecentSnapshot{Entries: []session.PersistentMessage{{MessageID: "m1", Role: "user"}}}})
		case "/session-history/page":
			if r.URL.Query().Get("cursor") != "next" || r.URL.Query().Get("limit") != "7" {
				t.Errorf("page query = %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(session.MessageHistoryPage{Messages: []session.PersistentMessage{{MessageID: "m1", Role: "user", ContentRef: &ref}}, SnapshotSequence: 9})
		case "/session-history/locate":
			if r.URL.Query().Get("messageId") != "m1" || r.URL.Query().Get("snapshot") != "9" {
				t.Errorf("locate query = %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(session.MessageLocation{Status: "ready", MessageID: "m1", SnapshotSequence: 9, Cursor: "located"})
		case "/session-history/content":
			var request struct {
				Ref    sessioncontent.Ref `json:"ref"`
				Offset int64              `json:"offset"`
				Length int64              `json:"length"`
			}
			if err := json.Unmarshal([]byte(r.URL.Query().Get("request")), &request); err != nil {
				t.Fatal(err)
			}
			if request.Ref.Digest != ref.Digest || request.Offset != 0 || request.Length != 3 {
				t.Errorf("content request = %+v", request)
			}
			_ = json.NewEncoder(w).Encode(SessionHistoryContentChunk{Data: base64.StdEncoding.EncodeToString([]byte("big")), NextOffset: 3, Done: true})
		case "/session-history/search":
			if r.URL.Query().Get("q") != "needle" || r.URL.Query().Get("cursor") != "older" || r.URL.Query().Get("limit") != "5" {
				t.Errorf("search query = %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(session.SearchHistoryPage{Hits: []session.SearchHistoryHit{{MessageID: "m1", Preview: "needle"}}, SnapshotSequence: 9})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	tab.capabilities = map[string]bool{serveCapabilitySessionContentV1: true, serveCapabilitySessionReadV2: true}
	tab.session.sessionID = "canonical"
	view, err := app.RemoteSessionOpenForTab(tab.id)
	if err != nil || view.SnapshotSequence != 9 || len(view.Recent.Entries) != 1 {
		t.Fatalf("open = %+v, %v", view, err)
	}
	page, err := app.RemoteSessionHistoryPageForTab(tab.id, "next", 7)
	if err != nil || page.SnapshotSequence != 9 || len(page.Messages) != 1 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	location, err := app.RemoteLocateSessionMessageForTab(tab.id, "m1", 9)
	if err != nil || location.Status != "ready" || location.Cursor != "located" {
		t.Fatalf("location = %+v, %v", location, err)
	}
	chunk, err := app.RemoteSessionHistoryContentForTab(tab.id, ref, 0)
	if err != nil || chunk.Data != base64.StdEncoding.EncodeToString([]byte("big")) || !chunk.Done {
		t.Fatalf("chunk = %+v, %v", chunk, err)
	}
	search, err := app.RemoteSearchSessionHistoryForTab(tab.id, "needle", "older", 5)
	if err != nil || len(search.Hits) != 1 || search.Hits[0].MessageID != "m1" {
		t.Fatalf("search = %+v, %v", search, err)
	}
}

func TestRemoteCanonicalSessionHistoryRequiresCapability(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reads.Add(1) }))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	if _, err := app.RemoteSessionHistoryPageForTab(tab.id, "", 0); err == nil {
		t.Fatal("canonical history unexpectedly enabled")
	}
	if reads.Load() != 0 {
		t.Fatalf("network reads = %d", reads.Load())
	}
}

// historyWindowFixture builds a remote tab that advertises exactly the
// capabilities the window protocol needs.
func historyWindowFixture(server *httptest.Server) (*App, *remoteTab) {
	app, tab := remoteTranscriptFixture(server)
	tab.session.sessionID = "canonical"
	tab.capabilities = map[string]bool{
		serveCapabilitySessionContentV1: true,
		serveCapabilityHistoryWindowV1:  true,
	}
	return app, tab
}

func TestRemoteSessionHistoryWindowRequiresCapability(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reads.Add(1) }))
	defer server.Close()

	// Without session-content-v1 the canonical routes are not negotiated at
	// all, so a window request must not reach the network.
	app, tab := remoteTranscriptFixture(server)
	tab.session.sessionID = "canonical"
	if page, err := app.RemoteSessionHistoryWindowForTab(tab.id, session.HistoryWindowRequest{Anchor: "newest"}); err != nil || page.Status != session.HistoryWindowUnsupported {
		t.Fatalf("window read without canonical history = %+v, %v", page, err)
	}

	// With content but without history-window-v1 the service is an older Serve:
	// a capability answer, not a failure. The caller gets a typed unsupported
	// status and still no round trip, so no service fakes a bounded window.
	tab.capabilities = map[string]bool{serveCapabilitySessionContentV1: true}
	page, err := app.RemoteSessionHistoryWindowForTab(tab.id, session.HistoryWindowRequest{Anchor: "newest"})
	if err != nil || page.Status != session.HistoryWindowUnsupported {
		t.Fatalf("unadvertised window = %+v, %v", page, err)
	}
	field, err := app.RemoteSessionMessageFieldForTab(tab.id, "m1", 0, "content", 0, 64)
	if err != nil || field.Status != session.HistoryWindowUnsupported || field.MessageID != "m1" {
		t.Fatalf("unadvertised field read = %+v, %v", field, err)
	}
	if reads.Load() != 0 {
		t.Fatalf("unadvertised capabilities issued %d requests", reads.Load())
	}
}

func TestRemoteSessionHistoryWindowSendsAnchorsAndReturnsTypedStatus(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session-history/window" {
			seen = append(seen, r.URL.Query())
			w.Header().Set("Content-Type", "application/json")
			page := session.HistoryWindowPage{Status: "ready", SnapshotSequence: 9, TotalTurns: 3,
				HasOlder: true, HasNewer: true, OlderCursor: "older", NewerCursor: "newer",
				AnchorMessageID: r.URL.Query().Get("messageId"),
				Messages:        []session.PersistentMessage{{MessageID: "m7", Position: 7, Version: 1, Role: "user"}},
			}
			_ = json.NewEncoder(w).Encode(page)
			return
		}
		if r.URL.Path == "/session-message-field" {
			seen = append(seen, r.URL.Query())
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(session.MessageFieldPage{Status: "ready", MessageID: "m7", Field: "content",
				TotalBytes: 100, Offset: 0, NextOffset: 64, Encoding: "utf-8", Data: []byte("fragment")})
			return
		}
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer server.Close()
	app, tab := historyWindowFixture(server)

	page, err := app.RemoteSessionHistoryWindowForTab(tab.id, session.HistoryWindowRequest{
		Anchor: "message", MessageID: "m7", Direction: "older", Limit: 32,
	})
	if err != nil || page.Status != "ready" || page.SnapshotSequence != 9 || len(page.Messages) != 1 {
		t.Fatalf("window = %+v, %v", page, err)
	}
	if page.AnchorMessageID != "m7" || !page.HasOlder || !page.HasNewer || page.OlderCursor != "older" || page.NewerCursor != "newer" {
		t.Fatalf("window metadata = %+v", page)
	}
	field, err := app.RemoteSessionMessageFieldForTab(tab.id, "m7", 3, "content", 0, 64)
	if err != nil || field.Status != "ready" || field.NextOffset != 64 || string(field.Data) != "fragment" {
		t.Fatalf("field = %+v, %v", field, err)
	}
	if len(seen) != 2 {
		t.Fatalf("requests = %d", len(seen))
	}
	// The tab's binding identity is stamped by the host, never by the caller:
	// a window read cannot name another session.
	for index, query := range seen {
		if query.Get("sessionId") != "canonical" {
			t.Fatalf("request %d session=%q", index, query.Get("sessionId"))
		}
	}
	if got := seen[0]; got.Get("anchor") != "message" || got.Get("messageId") != "m7" || got.Get("direction") != "older" || got.Get("limit") != "32" {
		t.Fatalf("window query = %v", got)
	}
	if got := seen[1]; got.Get("messageId") != "m7" || got.Get("field") != "content" || got.Get("version") != "3" || got.Get("offset") != "0" || got.Get("length") != "64" {
		t.Fatalf("field query = %v", got)
	}
}

// TestRemoteSessionHistoryWindowKeepsTypedStatuses pins the transport's
// contract with the reader: a stale cursor and a not-found anchor are answers
// the caller reasons about, not transport failures.
func TestRemoteSessionHistoryWindowKeepsTypedStatuses(t *testing.T) {
	for _, status := range []string{"stale_cursor", "not_found", "preparing", "failed"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(session.HistoryWindowPage{Status: status})
			}))
			defer server.Close()
			app, tab := historyWindowFixture(server)
			page, err := app.RemoteSessionHistoryWindowForTab(tab.id, session.HistoryWindowRequest{Anchor: "newest"})
			if err != nil || page.Status != status {
				t.Fatalf("status %q surfaced as %+v, %v", status, page, err)
			}
		})
	}
}

// TestSessionHistoryWindowRequiresCanonicalBinding keeps the local command off
// every path that has no exclusive canonical session behind it.
func TestSessionHistoryWindowRequiresCanonicalBinding(t *testing.T) {
	app := &App{}
	if _, err := app.SessionHistoryWindowForTab("missing", session.HistoryWindowRequest{Anchor: "newest"}); err == nil {
		t.Fatal("window read without a bound session")
	}
	if _, err := app.SessionMessageFieldForTab("missing", "m1", 0, "content", 0, 64); err == nil {
		t.Fatal("field read without a bound session")
	}
}
