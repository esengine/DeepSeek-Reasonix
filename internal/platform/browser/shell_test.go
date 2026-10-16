package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// shellStub is the smallest shell a ShellSession can drive: an HTTP server that
// answers the paths the kernel posts to, remembering what it was asked. It is
// the phone shell's half of the conversation, minus the WebView.
type shellStub struct {
	mu     sync.Mutex
	paths  []string
	bodies []string
	answer map[string]string
}

func newShellStub() *shellStub {
	return &shellStub{answer: map[string]string{}}
}

func (s *shellStub) serve(t *testing.T) *ShellSession {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.bodies = append(s.bodies, string(body))
		answer, ok := s.answer[r.URL.Path]
		var code int
		if ok {
			code = http.StatusOK
		} else {
			code, answer = http.StatusNotFound, `{"error":"not found"}`
		}
		s.mu.Unlock()
		w.WriteHeader(code)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return NewShellSession(srv.URL, nil, "")
}

// asked returns every path the stub was asked for, in order.
func (s *shellStub) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// lastBody is the body of the last request to a path, "" when it was never hit.
func (s *shellStub) lastBody(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ""
	for i, p := range s.paths {
		if p == path {
			out = s.bodies[i]
		}
	}
	return out
}

// The element refs the shell stamps are only worth issuing if the snapshot the
// model reads actually shows them; a ref nobody can see is a ref nobody uses.
func TestShellSnapshotShowsTheElementRefsItIssued(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"title":"T","url":"https://example.com/","text":"hello there",` +
		`"elements":[{"ref":"e1","role":"button","name":"Go"},` +
		`{"ref":"e2","role":"textbox","name":"Query","value":"cats"}]}`
	s := stub.serve(t)
	snap, err := s.Snapshot(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	joined := strings.Join(snap.Lines, "\n")
	for _, want := range []string{
		"hello there",
		"Elements you can act on:",
		`- button "Go" [e1]`,
		`- textbox "Query" [e2] value="cats"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the snapshot is missing %q:\n%s", want, joined)
		}
	}
}

// A page the shell could not load is not content: the read has to fail as a
// navigation failure rather than hand the browser's own error notice back as
// the site's own words.
func TestShellSnapshotReportsAPageTheShellCouldNotLoad(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"url":"https://nope.invalid/","title":"","reason":"ERR_NAME_NOT_RESOLVED","text":""}`
	s := stub.serve(t)
	if _, err := s.Snapshot(context.Background(), "", ""); CodeOf(err) != CodeNavigationFailed {
		t.Fatalf("a failed page read = %v, want %s", err, CodeNavigationFailed)
	}
}

// A ref names an element of the document the last snapshot read. One that was
// never issued, or that belonged to a page since navigated away from, must be
// refused rather than resolving to whatever now answers the same attribute.
func TestShellActRefusesARefTheLastSnapshotNeverIssued(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"url":"https://example.com/","text":"x",` +
		`"elements":[{"ref":"e1","role":"button","name":"Go"}]}`
	stub.answer["/click"] = `{"ok":true}`
	s := stub.serve(t)
	if _, err := s.Snapshot(context.Background(), "", ""); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	_, err := s.Act(context.Background(), "", []Step{{Action: "click", Ref: "e9"}}, false, "")
	if CodeOf(err) != CodeStaleRef {
		t.Fatalf("clicking e9 = %v, want %s", err, CodeStaleRef)
	}
	if got := stub.asked(); len(got) != 1 || got[0] != "/read" {
		t.Fatalf("a refused ref still reached the shell: %v", got)
	}
}

// A ref the snapshot did issue has to reach the page as a click on the element
// the shell stamped, which is the whole point of issuing it.
func TestShellActClicksTheElementARefNames(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"url":"https://example.com/","text":"x",` +
		`"elements":[{"ref":"e1","role":"button","name":"Go"}]}`
	stub.answer["/click"] = `{"ok":true}`
	s := stub.serve(t)
	if _, err := s.Snapshot(context.Background(), "", ""); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := s.Act(context.Background(), "", []Step{{Action: "click", Ref: "e1"}}, false, ""); err != nil {
		t.Fatalf("clicking e1: %v", err)
	}
	if body := stub.lastBody("/click"); !strings.Contains(body, `[data-rx-ref=\"e1\"]`) {
		t.Fatalf("the click did not name e1's attribute: %s", body)
	}
}

// A ref belongs to the document the snapshot read it from. After this view has
// moved to another document its numbering says nothing, so a ref from before
// the move must be refused rather than landing on whatever element of the new
// page answers the same attribute.
func TestShellNavigationDropsTheRefsOfThePageItLeft(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"url":"https://example.com/","text":"x",` +
		`"elements":[{"ref":"e1","role":"button","name":"Go"}]}`
	stub.answer["/load"] = `{"url":"https://example.com/other","title":"Other"}`
	stub.answer["/click"] = `{"ok":true}`
	s := stub.serve(t)
	if _, err := s.Snapshot(context.Background(), "", ""); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := s.Visit(context.Background(), "https://example.com/other", "", false); err != nil {
		t.Fatalf("Visit: %v", err)
	}
	if _, err := s.Act(context.Background(), "", []Step{{Action: "click", Ref: "e1"}}, false, ""); CodeOf(err) != CodeStaleRef {
		t.Fatalf("a ref from the page before navigating = %v, want %s", err, CodeStaleRef)
	}
}

// The same drop has to follow a reload and a step back: both replace the
// document under the refs the last snapshot read.
func TestShellBackAndReloadDropTheRefsToo(t *testing.T) {
	for _, action := range []string{"back", "forward", "reload"} {
		stub := newShellStub()
		stub.answer["/read"] = `{"url":"https://example.com/","text":"x",` +
			`"elements":[{"ref":"e1","role":"button","name":"Go"}]}`
		stub.answer["/"+action] = `{"ok":true}`
		stub.answer["/click"] = `{"ok":true}`
		s := stub.serve(t)
		if _, err := s.Snapshot(context.Background(), "", ""); err != nil {
			t.Fatalf("%s: Snapshot: %v", action, err)
		}
		if _, err := s.Act(context.Background(), "", []Step{{Action: action}}, false, ""); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if _, err := s.Act(context.Background(), "", []Step{{Action: "click", Ref: "e1"}}, false, ""); CodeOf(err) != CodeStaleRef {
			t.Fatalf("after %s, a ref from before = %v, want %s", action, err, CodeStaleRef)
		}
	}
}

// A click the page did not perform must not be reported as one: the model acts
// on "clicked e1" and would otherwise believe a page moved when it did not.
func TestShellActClickReportsAClickThatDidNotHappen(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"url":"https://example.com/","text":"x",` +
		`"elements":[{"ref":"e1","role":"button","name":"Go"}]}`
	// The element left the page between the snapshot and the click, which the
	// injected script reports as {ok:false}.
	stub.answer["/click"] = `{"ok":false}`
	s := stub.serve(t)
	if _, err := s.Snapshot(context.Background(), "", ""); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	_, err := s.Act(context.Background(), "", []Step{{Action: "click", Ref: "e1"}}, false, "")
	if CodeOf(err) != CodeStaleRef {
		t.Fatalf("a click that found nothing = %v, want %s", err, CodeStaleRef)
	}
}

// Clicking by CSS selector is the older path and stays: a step that names no
// ref but does name a selector is still a click this backend can run.
func TestShellActClickKeepsTheSelectorPath(t *testing.T) {
	stub := newShellStub()
	stub.answer["/click"] = `{"ok":true}`
	s := stub.serve(t)
	if _, err := s.Act(context.Background(), "", []Step{{Action: "click", Text: "#submit"}}, false, ""); err != nil {
		t.Fatalf("click by selector: %v", err)
	}
	if body := stub.lastBody("/click"); !strings.Contains(body, "#submit") {
		t.Fatalf("the selector did not reach the shell: %s", body)
	}
}

// Filling a ref the page no longer has must fail rather than type into whatever
// field happens to answer the selector now.
func TestShellActFillReportsARefThePageMovedPast(t *testing.T) {
	stub := newShellStub()
	stub.answer["/read"] = `{"url":"https://example.com/","text":"x",` +
		`"elements":[{"ref":"e1","role":"textbox","name":"Query"}]}`
	// The element is gone by the time the fill runs, which the injected script
	// reports as the empty string.
	stub.answer["/eval"] = `{"result":""}`
	s := stub.serve(t)
	if _, err := s.Snapshot(context.Background(), "", ""); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	_, err := s.Act(context.Background(), "", []Step{{Action: "fill", Ref: "e1", Text: "cats"}}, false, "")
	if CodeOf(err) != CodeStaleRef {
		t.Fatalf("filling a vanished element = %v, want %s", err, CodeStaleRef)
	}
}

// What a page asked the network for is reported, and each entry says the
// response is not visible here: the WebView's interceptor has no status to give,
// and an entry left looking "in flight" would be read as one still running.
func TestShellRequestsSayTheResponseIsNotVisibleHere(t *testing.T) {
	stub := newShellStub()
	stub.answer["/requests"] = `{"requests":[{"method":"GET","url":"https://example.com/app.js","kind":"script"}]}`
	s := stub.serve(t)
	requests, _, err := s.Requests("")
	if err != nil {
		t.Fatalf("Requests: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	r := requests[0]
	if r.Method != "GET" || r.URL != "https://example.com/app.js" || r.Kind != "script" {
		t.Fatalf("request = %+v", r)
	}
	if !r.ResponseUnknown {
		t.Fatal("a request whose response this backend cannot see was not marked as such")
	}
}
