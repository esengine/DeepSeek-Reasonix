package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/servecontract"
	canonical "reasonix/internal/session"
	"reasonix/internal/transcript"
)

func TestTranscriptHTTPBindsSessionAndImmutableContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("body", 20000)})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	executor := agent.New(nil, nil, session, agent.Options{}, bc)
	ctrl := control.New(control.Options{Executor: executor, SessionDir: dir, SessionPath: path, Sink: bc})
	defer ctrl.Close()
	server := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/transcript/snapshot?session=" + url.QueryEscape(path))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("snapshot status=%d cache=%q", response.StatusCode, response.Header.Get("Cache-Control"))
	}
	var snapshot transcript.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ProtocolVersion != 1 || len(snapshot.Records) != 2 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	ref := snapshot.Records[1].Refs[0]
	encoded, _ := json.Marshal(transcript.ContentRequest{ContentRef: ref})
	contentResponse, err := http.Get(server.URL + "/transcript/content?session=" + url.QueryEscape(path) + "&request=" + url.QueryEscape(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	defer contentResponse.Body.Close()
	var content transcript.ContentChunk
	if err := json.NewDecoder(contentResponse.Body).Decode(&content); err != nil || len(content.Data) != 64<<10 {
		t.Fatalf("content bytes=%d err=%v", len(content.Data), err)
	}
	wrong, err := http.Get(server.URL + "/transcript/snapshot?session=" + url.QueryEscape(filepath.Join(dir, "different.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-session status=%d", wrong.StatusCode)
	}
	malformed, err := http.Get(server.URL + "/transcript/page?request=%7B")
	if err != nil {
		t.Fatal(err)
	}
	malformed.Body.Close()
	if malformed.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed status=%d", malformed.StatusCode)
	}
}

// outlineLessController embeds the interface, not the concrete controller, so
// its method set is exactly SessionAPI and the optional outline capability is
// genuinely absent.
type outlineLessController struct{ control.SessionAPI }

func TestTranscriptOutlineHTTPPaginatesAndAdvertisesCapability(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	session := agent.NewSession("system")
	for i := range 4 {
		session.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("question %d", i)})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("answer %d", i)})
	}
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Executor: agent.New(nil, nil, session, agent.Options{}, bc), SessionDir: dir, SessionPath: path, Sink: bc})
	defer ctrl.Close()
	srv := New(ctrl, bc, config.ServeConfig{})
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	if !slices.Contains(srv.capabilities(), servecontract.TranscriptOutlineV1) {
		t.Fatalf("serve does not advertise the outline capability: %v", srv.capabilities())
	}

	read := func(request transcript.OutlineRequest) transcript.OutlinePage {
		t.Helper()
		encoded, _ := json.Marshal(request)
		response, err := http.Get(server.URL + "/transcript/outline?session=" + url.QueryEscape(path) + "&request=" + url.QueryEscape(string(encoded)))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("outline status=%d cache=%q", response.StatusCode, response.Header.Get("Cache-Control"))
		}
		var page transcript.OutlinePage
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	first := read(transcript.OutlineRequest{Entries: 3})
	if first.ProtocolVersion != transcript.ProtocolVersion || first.Total != 4 || len(first.Entries) != 3 || first.Done {
		t.Fatalf("first outline page = %+v", first)
	}
	if first.Entries[0].Prompt != "question 0" || first.Entries[0].Answer != "answer 0" || first.Entries[0].Turn != 1 {
		t.Fatalf("first entry = %+v", first.Entries[0])
	}
	second := read(transcript.OutlineRequest{SnapshotID: first.SnapshotID, Offset: first.NextOffset, Entries: 3})
	if second.SnapshotID != first.SnapshotID || len(second.Entries) != 1 || !second.Done || second.Entries[0].Turn != 4 {
		t.Fatalf("second outline page = %+v", second)
	}

	// An evicted or unknown cut reports staleness instead of repositioning.
	if stale := read(transcript.OutlineRequest{SnapshotID: "evicted"}); !stale.Stale {
		t.Fatalf("unknown cut was answered as current: %+v", stale)
	}

	// Session binding failures stay conflicts, not empty outlines.
	wrong, err := http.Get(server.URL + "/transcript/outline?session=" + url.QueryEscape(filepath.Join(dir, "different.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-session status=%d", wrong.StatusCode)
	}

	// A controller without the optional capability declines the route so a
	// client can fall back to its loaded-turn rail.
	plain := httptest.NewServer(New(outlineLessController{ctrl}, bc, config.ServeConfig{}).Handler())
	defer plain.Close()
	unsupported, err := http.Get(plain.URL + "/transcript/outline?session=" + url.QueryEscape(path))
	if err != nil {
		t.Fatal(err)
	}
	unsupported.Body.Close()
	if unsupported.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unsupported status=%d", unsupported.StatusCode)
	}
}

func TestCanonicalSessionHistoryHTTPUsesAuthorizedContentRanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := canonical.NewService("serve", canonical.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), canonical.CreateOptions{SessionID: "canonical"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "large", Role: provider.RoleUser, Content: strings.Repeat("range", 20_000)}})
	if _, err := runtime.Session().AppendBatch(t.Context(), "message", []canonical.Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{SessionService: service, SessionRuntime: runtime, ExclusiveSession: true, Sink: bc})
	defer ctrl.Close()
	server := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer server.Close()
	openResponse, err := http.Get(server.URL + "/session/open?sessionId=canonical")
	if err != nil {
		t.Fatal(err)
	}
	defer openResponse.Body.Close()
	var openView canonical.SessionOpenView
	if err := json.NewDecoder(openResponse.Body).Decode(&openView); err != nil || openResponse.StatusCode != http.StatusOK || len(openView.Recent.Entries) != 1 {
		t.Fatalf("open status=%d view=%+v err=%v", openResponse.StatusCode, openView, err)
	}
	// Exercise the actual HTTP Follow contract against the canonical runtime,
	// including subscription disposal rather than leaving a long poll behind.
	followResponse, err := http.Get(server.URL + "/transcript/follow")
	if err != nil {
		t.Fatal(err)
	}
	var followed control.TranscriptFollowResponse
	err = json.NewDecoder(followResponse.Body).Decode(&followed)
	_ = followResponse.Body.Close()
	if err != nil || followResponse.StatusCode != http.StatusOK || followed.ProtocolVersion != 2 || followed.Snapshot == nil || followed.History == nil || followed.History.Status != "ready" {
		t.Fatalf("follow status=%d response=%+v error=%v", followResponse.StatusCode, followed, err)
	}
	closeRequest, _ := json.Marshal(transcript.FollowRequest{Subscription: followed.Subscription, Close: true})
	closed, err := http.Get(server.URL + "/transcript/follow?request=" + url.QueryEscape(string(closeRequest)))
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Body.Close()
	if closed.StatusCode != http.StatusOK {
		t.Fatalf("close follow status=%d", closed.StatusCode)
	}
	var page canonical.MessageHistoryPage
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		response, requestErr := http.Get(server.URL + "/session-history/page?sessionId=canonical&limit=10")
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&page)
		_ = response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("history status=%d page=%+v err=%v", response.StatusCode, page, decodeErr)
		}
		if page.Status == "ready" {
			break
		}
		if page.Status != "preparing" || time.Now().After(deadline) {
			t.Fatalf("history preparation = %+v", page)
		}
	}
	if len(page.Messages) != 1 || page.Messages[0].ContentRef == nil {
		t.Fatalf("history page=%+v", page)
	}
	locationResponse, err := http.Get(server.URL + "/session-history/locate?sessionId=canonical&messageId=large&snapshot=" + fmt.Sprint(page.SnapshotSequence))
	if err != nil {
		t.Fatal(err)
	}
	defer locationResponse.Body.Close()
	var location canonical.MessageLocation
	if err := json.NewDecoder(locationResponse.Body).Decode(&location); err != nil || locationResponse.StatusCode != http.StatusOK || location.Status != "ready" || location.Cursor == "" {
		t.Fatalf("location status=%d response=%+v err=%v", locationResponse.StatusCode, location, err)
	}
	var search canonical.SearchHistoryPage
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		response, requestErr := http.Get(server.URL + "/session-history/search?sessionId=canonical&q=range&limit=10")
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&search)
		_ = response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("search status=%d page=%+v err=%v", response.StatusCode, search, decodeErr)
		}
		if search.Status == "ready" {
			break
		}
		if search.Status != "preparing" || time.Now().After(deadline) {
			t.Fatalf("search preparation = %+v", search)
		}
	}
	if len(search.Hits) != 1 || search.Hits[0].MessageID != "large" {
		t.Fatalf("search page=%+v", search)
	}
	request, _ := json.Marshal(sessionHistoryContentRequest{Ref: *page.Messages[0].ContentRef, Offset: 0, Length: 32})
	contentResponse, err := http.Get(server.URL + "/session-history/content?sessionId=canonical&request=" + url.QueryEscape(string(request)))
	if err != nil {
		t.Fatal(err)
	}
	defer contentResponse.Body.Close()
	var content sessionHistoryContentResponse
	if err := json.NewDecoder(contentResponse.Body).Decode(&content); err != nil || contentResponse.StatusCode != http.StatusOK || content.Data == "" || content.NextOffset != 32 {
		t.Fatalf("content status=%d response=%+v err=%v", contentResponse.StatusCode, content, err)
	}
	wrong, err := http.Get(server.URL + "/session-history/page?sessionId=other")
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("wrong session status=%d", wrong.StatusCode)
	}
}

// TestCanonicalSessionHistoryWindowHTTPPagesBothDirections exercises
// history-window-v1: a newest page, both continuation cursors, and a
// message anchor that resolves through the index instead of walking pages.
func TestCanonicalSessionHistoryWindowHTTPPagesBothDirections(t *testing.T) {
	server, _ := newWindowTestServer(t)

	var first canonical.HistoryWindowPage
	getWindow(t, server, "anchor=newest&limit=4", &first)
	if len(first.Messages) != 4 || !first.HasOlder || first.OlderCursor == "" {
		t.Fatalf("newest window=%+v", windowShape(first))
	}
	if first.HasNewer || first.NewerCursor != "" {
		t.Fatalf("newest window must not page newer: %+v", windowShape(first))
	}
	// The page ends at the newest message: the anchor only ever moves backward.
	if got := first.Messages[len(first.Messages)-1].MessageID; got != "m16" {
		t.Fatalf("newest page tail=%q", got)
	}

	// Paging older from the newest page walks strictly backward and keeps the
	// snapshot pinned, so appends cannot invalidate the cursor.
	var older canonical.HistoryWindowPage
	getWindow(t, server, "anchor=cursor&cursor="+url.QueryEscape(first.OlderCursor)+"&direction=older&limit=4", &older)
	if got := windowIDs(older); !slices.Equal(got, []string{"m9", "m10", "m11", "m12"}) {
		t.Fatalf("older page ids=%v", got)
	}
	if older.SnapshotSequence != first.SnapshotSequence {
		t.Fatalf("cursor page moved snapshot %d -> %d", first.SnapshotSequence, older.SnapshotSequence)
	}

	// Paging newer from that same page returns exactly the page we came from.
	var newer canonical.HistoryWindowPage
	getWindow(t, server, "anchor=cursor&cursor="+url.QueryEscape(older.NewerCursor)+"&direction=newer&limit=4", &newer)
	if got := windowIDs(newer); !slices.Equal(got, []string{"m13", "m14", "m15", "m16"}) {
		t.Fatalf("newer page ids=%v", got)
	}

	// A message anchor lands on a window around that message in one round trip
	// (no newest-first walk): older paging ends at the anchor itself.
	var anchored canonical.HistoryWindowPage
	getWindow(t, server, "anchor=message&messageId=m6&direction=older&limit=3", &anchored)
	if got := windowIDs(anchored); !slices.Equal(got, []string{"m4", "m5", "m6"}) {
		t.Fatalf("message anchor ids=%v", got)
	}
	if anchored.AnchorMessageID != "m6" || !anchored.HasNewer {
		t.Fatalf("message anchor metadata=%+v", windowShape(anchored))
	}
	if anchored.SnapshotSequence != first.SnapshotSequence {
		t.Fatalf("anchor left the pinned snapshot: %d", anchored.SnapshotSequence)
	}

	// A turn anchor resolves through the same index.
	var byTurn canonical.HistoryWindowPage
	getWindow(t, server, "anchor=turn&turn=3&direction=older&limit=2", &byTurn)
	if byTurn.AnchorTurn != 3 || len(byTurn.Messages) == 0 {
		t.Fatalf("turn anchor=%+v", windowShape(byTurn))
	}
	for _, message := range byTurn.Messages {
		if message.VisibleTurn > 3 {
			t.Fatalf("turn anchor leaked newer turn %d", message.VisibleTurn)
		}
	}
}

// TestCanonicalSessionHistoryWindowHTTPRejectsForeignCursor keeps the cursor a
// bound credential: another session's cursor is stale, not a silent re-anchor.
func TestCanonicalSessionHistoryWindowHTTPRejectsForeignCursor(t *testing.T) {
	server, _ := newWindowTestServer(t)
	var page canonical.HistoryWindowPage
	getWindow(t, server, "anchor=newest&limit=2", &page)
	raw, err := base64.RawURLEncoding.DecodeString(page.OlderCursor)
	if err != nil {
		t.Fatalf("cursor is not raw-url base64: %v", err)
	}
	var bound map[string]any
	if err := json.Unmarshal(raw, &bound); err != nil {
		t.Fatalf("cursor is not JSON: %v", err)
	}
	if bound["sessionId"] != "canonical" {
		t.Fatalf("cursor does not name its session: %v", bound["sessionId"])
	}
	bound["sessionId"] = "elsewhere"
	reissued, err := json.Marshal(bound)
	if err != nil {
		t.Fatal(err)
	}
	foreign := base64.RawURLEncoding.EncodeToString(reissued)
	var rejected canonical.HistoryWindowPage
	getWindow(t, server, "anchor=cursor&cursor="+url.QueryEscape(foreign)+"&limit=2", &rejected)
	if rejected.Status != "stale_cursor" || len(rejected.Messages) != 0 {
		t.Fatalf("foreign cursor status=%q messages=%d", rejected.Status, len(rejected.Messages))
	}
	var malformed canonical.HistoryWindowPage
	getWindow(t, server, "anchor=cursor&cursor=not-a-cursor&limit=2", &malformed)
	if malformed.Status != "stale_cursor" {
		t.Fatalf("malformed cursor status=%q", malformed.Status)
	}
}

// TestCanonicalSessionMessageFieldHTTPStreamsAlignedFragments verifies the
// per-field read: a bounded fragment, a total length, and concatenated ranges
// that re-parse as the original value.
func TestCanonicalSessionMessageFieldHTTPStreamsAlignedFragments(t *testing.T) {
	server, _ := newWindowTestServer(t)
	var page canonical.HistoryWindowPage
	getWindow(t, server, "anchor=message&messageId=m2&direction=older&limit=1", &page)
	if len(page.Messages) != 1 || page.Messages[0].ContentRef == nil {
		t.Fatalf("anchored page=%+v", windowShape(page))
	}
	// The window issues the content grant this cell reads against; a body over
	// the inline preview budget must come back referenced, not inlined.
	if ref := page.Messages[0].ContentRef; ref.Digest == "" || ref.Bytes == 0 || page.Messages[0].Inline != nil {
		t.Fatalf("content reference=%+v inline=%v", ref, page.Messages[0].Inline)
	}

	var assembled strings.Builder
	var offset int64
	// Rune- and escape-safe cutting is a property of the cut size, not of the
	// number of cuts: the first reads use a length far below one CJK rune's
	// width to force mid-rune boundaries, then the remainder drains in large
	// fragments. Requesting the whole body 64 bytes at a time would cost
	// thousands of localhost round trips for no additional coverage.
	const narrowLength, narrowReads, wideLength = 64, 8, 1 << 16
	for reads := 0; ; reads++ {
		length := int64(wideLength)
		if reads < narrowReads {
			length = narrowLength
		}
		request := url.Values{"sessionId": []string{"canonical"}, "messageId": []string{"m2"}, "field": []string{"content"}}
		request.Set("offset", fmt.Sprint(offset))
		request.Set("length", fmt.Sprint(length))
		response, err := http.Get(server.URL + "/session-message-field?" + request.Encode())
		if err != nil {
			t.Fatal(err)
		}
		var fragment canonical.MessageFieldPage
		decodeErr := json.NewDecoder(response.Body).Decode(&fragment)
		_ = response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("field status=%d page=%+v err=%v", response.StatusCode, fragment, decodeErr)
		}
		if fragment.Status != "ready" || fragment.Encoding != "utf-8" {
			t.Fatalf("field page=%+v", fragment)
		}
		// TotalBytes counts the field's JSON source, quotes included, not the
		// decoded value and not the whole canonical body.
		if want := int64(len(windowTestBody) + len(`""`)); fragment.TotalBytes != want {
			t.Fatalf("total bytes=%d want %d", fragment.TotalBytes, want)
		}
		if int64(len(fragment.Data)) > length {
			t.Fatalf("fragment exceeded the requested length %d: %d", length, len(fragment.Data))
		}
		assembled.Write(fragment.Data)
		if fragment.NextOffset == 0 {
			break
		}
		if fragment.NextOffset <= offset {
			t.Fatalf("field offset did not advance: %d -> %d", offset, fragment.NextOffset)
		}
		offset = fragment.NextOffset
		if offset > 1<<20 {
			t.Fatal("field read did not terminate")
		}
	}
	// Concatenated fragments must re-parse as the original value: the source
	// form is JSON, and a cut inside a rune or an escape would break this.
	var decoded string
	if err := json.Unmarshal([]byte(assembled.String()), &decoded); err != nil {
		t.Fatalf("reassembled field is not valid JSON: %v", err)
	}
	if decoded != windowTestBody {
		t.Fatalf("reassembled field=%d runes want %d", len([]rune(decoded)), len([]rune(windowTestBody)))
	}

	// A field the message does not carry reads as an empty, finished fragment
	// rather than an error, so the client can distinguish it from a failure.
	missing, err := http.Get(server.URL + "/session-message-field?sessionId=canonical&messageId=m2&field=reasoning_content")
	if err != nil {
		t.Fatal(err)
	}
	var absent canonical.MessageFieldPage
	decodeErr := json.NewDecoder(missing.Body).Decode(&absent)
	_ = missing.Body.Close()
	if decodeErr != nil || missing.StatusCode != http.StatusOK || absent.Status != "ready" || absent.TotalBytes != 0 {
		t.Fatalf("absent field status=%d page=%+v err=%v", missing.StatusCode, absent, decodeErr)
	}

	unknown, err := http.Get(server.URL + "/session-message-field?sessionId=canonical&messageId=nope&field=content")
	if err != nil {
		t.Fatal(err)
	}
	var notFound canonical.MessageFieldPage
	decodeErr = json.NewDecoder(unknown.Body).Decode(&notFound)
	_ = unknown.Body.Close()
	if decodeErr != nil || unknown.StatusCode != http.StatusOK || notFound.Status != "not_found" {
		t.Fatalf("unknown message status=%d page=%+v err=%v", unknown.StatusCode, notFound, decodeErr)
	}
}

// windowTestBody is m2's content: larger than the 32 KiB inline preview
// budget so the window returns a content reference, and non-ASCII so a
// rune-splitting bug cannot pass by accident.
var windowTestBody = strings.Repeat("分块读取正文内容", 6000)

func newWindowTestServer(t *testing.T) (*httptest.Server, *control.Controller) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := canonical.NewService("serve", canonical.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), canonical.CreateOptions{SessionID: "canonical"})
	if err != nil {
		t.Fatal(err)
	}
	events := make([]canonical.Event, 0, 16)
	for index := 1; index <= 16; index++ {
		role, content := provider.RoleUser, fmt.Sprintf("question %d", index)
		if index%2 == 0 {
			role, content = provider.RoleAssistant, fmt.Sprintf("answer %d", index)
			if index == 2 {
				// Larger than the inline preview budget, so the window hands
				// back a content reference and the field route has to stream.
				content = windowTestBody
			}
		}
		payload, err := json.Marshal(map[string]any{"message": provider.Message{ID: fmt.Sprintf("m%d", index), Role: role, Content: content}})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, canonical.Event{Kind: "message/complete", Payload: payload})
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "message", events); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{SessionService: service, SessionRuntime: runtime, ExclusiveSession: true, Sink: bc})
	t.Cleanup(ctrl.Close)
	server := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	t.Cleanup(server.Close)
	return server, ctrl
}

// getWindow polls the window route out of "preparing": the first read of a
// cold session kicks off the locator build in the background.
func getWindow(t *testing.T, server *httptest.Server, query string, into *canonical.HistoryWindowPage) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get(server.URL + "/session-history/window?sessionId=canonical&" + query)
		if err != nil {
			t.Fatal(err)
		}
		decodeErr := json.NewDecoder(response.Body).Decode(into)
		_ = response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("window status=%d page=%+v err=%v", response.StatusCode, into, decodeErr)
		}
		if into.Status != "preparing" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("window stayed preparing for %q", query)
		}
		time.Sleep(time.Millisecond)
	}
}

func windowIDs(page canonical.HistoryWindowPage) []string {
	ids := make([]string, 0, len(page.Messages))
	for _, message := range page.Messages {
		ids = append(ids, message.MessageID)
	}
	return ids
}

// windowShape keeps failure output readable: pages carry full bodies.
func windowShape(page canonical.HistoryWindowPage) canonical.HistoryWindowPage {
	page.Messages = nil
	return page
}
