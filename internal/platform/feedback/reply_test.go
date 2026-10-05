package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type replyStub struct {
	*stub
	mu2    sync.Mutex
	hits   []replyHit
	answer func(n int, w http.ResponseWriter, r *http.Request)
	nextID int64
	// The worker's reply rules: status per receipt (default needs_info), at
	// most 10 replies per report and hourly per install (default 3).
	status map[string]string
	count  map[string]int
	hourly int
	sent   int
}

type replyHit struct {
	path, id, token string
	body            string
}

func (r *replyStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/feedback/{receipt}/reply", func(w http.ResponseWriter, req *http.Request) {
		var b struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(req.Body).Decode(&b)
		r.mu2.Lock()
		r.hits = append(r.hits, replyHit{req.URL.Path, req.Header.Get("X-Install-Id"), req.Header.Get("X-Install-Token"), b.Body})
		n := len(r.hits)
		r.mu2.Unlock()
		if r.answer != nil {
			r.answer(n, w, req)
			return
		}
		if !r.admitReply(w, req, b.Body) {
			return
		}
		r.nextID++
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"replyId":`+strconv.FormatInt(r.nextID+100, 10)+`,"createdAt":"2026-10-02T09:00:00Z"}`)
	})
	mux.Handle("/", r.stub.handler())
	return mux
}

var workerReceipt = regexp.MustCompile(`^FB-[A-Z0-9]{4}-[A-Z0-9]{4}$`)

// admitReply applies the worker's rules in its order and answers a refusal
// itself; it reports whether the reply was taken.
func (r *replyStub) admitReply(w http.ResponseWriter, req *http.Request, body string) bool {
	r.mu2.Lock()
	defer r.mu2.Unlock()
	receipt := req.PathValue("receipt")
	limit := r.hourly
	if limit == 0 {
		limit = 3
	}
	status := "needs_info"
	if v, ok := r.status[receipt]; ok {
		status = v
	}
	switch {
	case !workerReceipt.MatchString(receipt):
		w.WriteHeader(http.StatusNotFound)
	case req.Header.Get("X-Install-Token") != stubToken(req.Header.Get("X-Install-Id")):
		stubRefuse(w, http.StatusUnauthorized, "feedback.bad_token")
	case len(body) > 4096 || strings.TrimSpace(body) == "":
		stubRefuse(w, http.StatusBadRequest, "feedback.invalid")
	case !slices.Contains([]string{"needs_info", "answered", "recorded", "in_progress"}, status):
		stubRefuse(w, http.StatusConflict, "feedback.not_replyable")
	case r.count[receipt] >= 10:
		stubRefuse(w, http.StatusTooManyRequests, "feedback.reply_limit")
	case r.sent >= limit:
		w.Header().Set("Retry-After", "30")
		stubRefuse(w, http.StatusTooManyRequests, "feedback.rate_limited")
	default:
		if r.count == nil {
			r.count = map[string]int{}
		}
		r.count[receipt]++
		r.sent++
		return true
	}
	return false
}

func replySetup(t *testing.T) (*Service, *replyStub) {
	t.Helper()
	rs := &replyStub{stub: &stub{}}
	srv := httptest.NewServer(rs.handler())
	t.Cleanup(srv.Close)
	home := t.TempDir()
	svc, err := New(Config{Home: home, Base: srv.URL, HTTP: srv.Client(), Backoff: []time.Duration{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	return svc, rs
}

const threadMine = `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"Sidebar loses selection","status":"needs_info","needsInput":true,
"replies":[{"id":7,"author":"maintainer","body":"Which OS?","createdAt":"2026-10-01T08:00:00Z"},{"id":8,"author":"user","body":"mac","createdAt":"2026-10-01T09:00:00Z"},{"id":9,"author":"maintainer","body":"Steps please","createdAt":"2026-10-01T10:00:00Z"}],
"createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-01T10:00:00Z"}]}`

func TestReplySendsIdentityPathAndRedactedBody(t *testing.T) {
	svc, rs := replySetup(t)
	got, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "  steps: click api_key=sk-abcdefghijklmnopqrstuvwxyz  ")
	if err != nil || got.ReplyID == 0 {
		t.Fatalf("reply = %+v %v", got, err)
	}
	h := rs.hits[0]
	if h.path != "/v1/feedback/FB-7K3M-9QX2/reply" || h.id != rs.posts[0].InstallID || h.token != stubToken(h.id) {
		t.Fatalf("hit = %+v", h)
	}
	if strings.Contains(h.body, "sk-abcdef") || !strings.HasPrefix(h.body, "steps: click") {
		t.Fatalf("body on the wire = %q", h.body)
	}
}

func TestReplyRefusesBeforeSending(t *testing.T) {
	svc, rs := replySetup(t)
	cases := []struct {
		receipt, body, field, reason string
	}{
		{"FB-7K3M-9QX2", "   ", FieldBody, ReasonEmpty},
		{"FB-7K3M-9QX2", strings.Repeat("ab ", DefaultLimits.ReplyBytes/3+1), FieldBody, ReasonTooLong},
		{"../admin", "hi", FieldReceipt, ReasonBadValue},
		{"FB-NONE-0000", "hi", FieldReceipt, ReasonBadValue},
	}
	for _, c := range cases {
		_, err := svc.Reply(context.Background(), c.receipt, c.body)
		var inv *InvalidError
		if !errors.As(err, &inv) || inv.Field != c.field || inv.Reason != c.reason {
			t.Errorf("%q/%d bytes: err = %v", c.receipt, len(c.body), err)
		}
	}
	if len(rs.hits) != 0 {
		t.Fatalf("%d requests left the machine", len(rs.hits))
	}
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", strings.Repeat("a", 1)+strings.Repeat("ab ", (DefaultLimits.ReplyBytes-1)/3)); err != nil {
		t.Fatalf("a reply of exactly the limit: %v", err)
	}
}

func TestReplyCodesBecomeSentinels(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{403, "feedback.challenge_required", ErrChallengeRequired},
		{503, "feedback.disabled", ErrDisabled},
		{502, "", ErrUnavailable},
	}
	for _, c := range cases {
		svc, rs := replySetup(t)
		rs.answer = func(_ int, w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, `{"error":{"code":"`+c.code+`","message":"these words are never matched"}}`)
		}
		_, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi")
		if !errors.Is(err, c.want) {
			t.Errorf("%d %s: err = %v, want %v", c.status, c.code, err, c.want)
		}
		it, _ := svc.Item("FB-7K3M-9QX2")
		if len(it.Replies) != 0 {
			t.Errorf("%s: a refused reply was remembered as sent", c.code)
		}
	}
}

func TestReplyIsNeverRetriedAfterAnUnansweredRequest(t *testing.T) {
	svc, rs := replySetup(t)
	svc.backoff = []time.Duration{time.Millisecond, time.Millisecond}
	tr := &countingTransport{next: svc.http.Transport}
	svc.http = &http.Client{Transport: tr}
	rs.answer = func(_ int, w http.ResponseWriter, _ *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}
	_, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi")
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v", err)
	}
	rs.mu2.Lock()
	reached := len(rs.hits)
	rs.mu2.Unlock()
	if tr.calls.Load() != 1 || reached != 1 {
		t.Fatalf("the reply was attempted %d times and reached the service %d times", tr.calls.Load(), reached)
	}
}

func TestChallengeTokenRidesTheSubmit(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	d := draft()
	d.TurnstileToken = " tok-abc "
	if _, err := svc.Submit(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if st.posts[0].TurnstileToken != "tok-abc" {
		t.Fatalf("token on the wire = %q", st.posts[0].TurnstileToken)
	}
	st2 := &stub{postFn: apiError(403, "feedback.challenge_required")}
	svc2, _ := setup(t, st2)
	if _, err := svc2.Submit(context.Background(), draft()); !errors.Is(err, ErrChallengeRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestMineCarriesThreadStatusesAndNeedsInput(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	got, err := svc.ListMine(context.Background())
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("mine = %+v %v", got, err)
	}
	it := got.Items[0]
	if it.Status != StatusNeedsInfo || !it.NeedsInput || len(it.Replies) != 3 || it.Replies[1].Author != AuthorUser || it.Replies[2].Body != "Steps please" {
		t.Fatalf("item = %+v", it)
	}
	for _, status := range []Status{StatusAnswered, StatusClosed, StatusReceived} {
		rs.mineBody = strings.Replace(threadMine, `"status":"needs_info"`, `"status":"`+string(status)+`"`, 1)
		got, _ = svc.ListMine(context.Background())
		if got.Items[0].Status != status {
			t.Errorf("status %s came back as %s", status, got.Items[0].Status)
		}
	}
}

func TestUnreadIsMaintainerRepliesNewerThanLastSeenAndSurvivesARestart(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = strings.Replace(threadMine, `"needsInput":true`, `"needsInput":false`, 1)
	got, _ := svc.ListMine(context.Background())
	if got.Items[0].UnreadReplies != 2 || got.Unread != 1 || !got.HasNew {
		t.Fatalf("before reading: %+v", got)
	}
	if err := svc.MarkSeen("FB-7K3M-9QX2", 9); err != nil {
		t.Fatal(err)
	}
	again, err := New(Config{Home: svc.store.path[:len(svc.store.path)-len(stateFile)], Base: svc.base, HTTP: svc.http, Backoff: []time.Duration{}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = again.ListMine(context.Background())
	if got.Items[0].UnreadReplies != 0 || got.Unread != 0 || got.HasNew {
		t.Fatalf("after reading and a restart: %+v", got)
	}
	rs.mineBody = strings.Replace(rs.mineBody, `"createdAt":"2026-09-30T08:00:00Z"`, `"x":0,"createdAt":"2026-09-30T08:00:00Z"`, 1)
	rs.mineBody = strings.Replace(rs.mineBody, `{"id":9,`, `{"id":12,"author":"maintainer","body":"fixed","createdAt":"2026-10-02T08:00:00Z"},{"id":9,`, 1)
	got, _ = again.ListMine(context.Background())
	if got.Items[0].UnreadReplies != 1 || !got.HasNew {
		t.Fatalf("a newer reply after reading: %+v", got)
	}
}

func TestNeedsInputAloneRaisesTheBadgeAndSeenDoesNotClearIt(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	_, _ = svc.ListMine(context.Background())
	_ = svc.MarkSeen("FB-7K3M-9QX2", 9)
	got, _ := svc.ListMine(context.Background())
	if got.Items[0].UnreadReplies != 0 || got.Unread != 1 || !got.HasNew {
		t.Fatalf("mine = %+v", got)
	}
}

func TestOfflineBadgeComesFromWhatIsRemembered(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	_, _ = svc.ListMine(context.Background())
	rs.answer = nil
	svc.http = &http.Client{Transport: failingTransport{}}
	got, err := svc.ListMine(context.Background())
	if err != nil || !got.Offline || !got.HasNew || got.Unread != 1 || got.Items[0].UnreadReplies != 2 {
		t.Fatalf("offline mine = %+v %v", got, err)
	}
}

type countingTransport struct {
	next  http.RoundTripper
	calls atomic.Int32
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.next.RoundTrip(r)
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("down")
}

func TestReplyUpdatesWhatIsRemembered(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	_, _ = svc.ListMine(context.Background())
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "steps: 1 2 3"); err != nil {
		t.Fatal(err)
	}
	it, _ := svc.Item("FB-7K3M-9QX2")
	last := it.Replies[len(it.Replies)-1]
	if last.Author != AuthorUser || last.Body != "steps: 1 2 3" || it.Status != StatusReceived || it.NeedsInput || it.UnreadReplies != 2 {
		t.Fatalf("after replying: %+v", it)
	}
}

func TestThreadTextIsCleanedOfEscapesAndBidiControls(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"answered",
"replies":[{"id":1,"author":"maintainer","body":"ok\u001b[31mred\u001b]0;title\u0007‮evil\r\nline2\u0000\ttab","createdAt":"2026-10-01T08:00:00Z"},{"id":2,"author":"root","body":"x","createdAt":"2026-10-01T08:00:00Z"}],
"createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-01T08:00:00Z"}]}`
	got, _ := svc.ListMine(context.Background())
	r := got.Items[0].Replies
	if r[0].Body != "okredevil\nline2 tab" {
		t.Fatalf("body = %q", r[0].Body)
	}
	if r[1].Author != AuthorUser {
		t.Fatalf("an unknown author reads as %q", r[1].Author)
	}
}

func TestAnItemFromARetiredIdentityTakesNoReplyAndRaisesNoBadge(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	_, _ = svc.ListMine(context.Background())
	_ = svc.store.update(func(st *state) error { st.retire(); return nil })
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi"); !errors.Is(err, ErrNotReplyable) {
		t.Fatalf("err = %v", err)
	}
	got, _ := svc.ListMine(context.Background())
	if got.Items[0].NeedsInput || got.Unread != 0 || got.HasNew || got.Items[0].UnreadReplies != 2 {
		t.Fatalf("a report nobody can answer still raises the badge: %+v", got)
	}
}

func TestSeenForgetsReportsThatFellOutOfTheLocalList(t *testing.T) {
	st := &state{Seen: map[string]ReplyID{"FB-GONE": 4, "FB-KEPT": 2}}
	st.remember(Item{Receipt: "FB-KEPT", CreatedAt: time.Now()})
	if _, ok := st.Seen["FB-GONE"]; ok || st.Seen["FB-KEPT"] != 2 {
		t.Fatalf("seen = %v", st.Seen)
	}
}

func TestReplyIDsReadAsNumbersOrNumericStrings(t *testing.T) {
	var got struct {
		Replies []Reply `json:"replies"`
		Receipt ReplyReceipt
	}
	if err := json.Unmarshal([]byte(`{"replies":[{"id":7,"author":"maintainer","body":"a"},{"id":"8","author":"user","body":"b"}]}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Replies[0].ID != 7 || got.Replies[1].ID != 8 {
		t.Fatalf("ids = %+v", got.Replies)
	}
	var rr ReplyReceipt
	if err := json.Unmarshal([]byte(`{"replyId":"12"}`), &rr); err != nil || rr.ReplyID != 12 {
		t.Fatalf("receipt = %+v %v", rr, err)
	}
	if err := json.Unmarshal([]byte(`{"replyId":"x"}`), &rr); err == nil {
		t.Fatal("a non-numeric id was accepted")
	}
}

func TestTheWorkersRulesDecideWhichRefusalComesBack(t *testing.T) {
	ctx := context.Background()
	svc, rs := replySetup(t)
	rs.hourly = 100
	rs.status = map[string]string{"FB-7K3M-9QX2": "closed"}
	if _, err := svc.Reply(ctx, "FB-7K3M-9QX2", "hi"); !errors.Is(err, ErrNotReplyable) {
		t.Fatalf("a closed report: %v", err)
	}
	rs.status = nil
	for i := range 10 {
		if _, err := svc.Reply(ctx, "FB-7K3M-9QX2", "hi"); err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
	}
	if _, err := svc.Reply(ctx, "FB-7K3M-9QX2", "hi"); !errors.Is(err, ErrReplyLimit) {
		t.Fatalf("the eleventh reply: %v", err)
	}
	svc2, rs2 := replySetup(t)
	for range 3 {
		if _, err := svc2.Reply(ctx, "FB-7K3M-9QX2", "hi"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := svc2.Reply(ctx, "FB-7K3M-9QX2", "hi")
	if !errors.Is(err, ErrRateLimited) || RetryAfter(err) != 30*time.Second || len(rs2.hits) != 4 {
		t.Fatalf("the fourth reply within the hour: %v", err)
	}
}

func TestALowercaseReceiptIsSentAndRememberedAsTheStoredOne(t *testing.T) {
	svc, rs := replySetup(t)
	if _, err := svc.Reply(context.Background(), "fb-7k3m-9qx2", "hi"); err != nil {
		t.Fatal(err)
	}
	if rs.hits[0].path != "/v1/feedback/FB-7K3M-9QX2/reply" {
		t.Fatalf("path = %s", rs.hits[0].path)
	}
	if it, _ := svc.Item("fb-7k3m-9qx2"); len(it.Replies) != 1 {
		t.Fatalf("the local thread was not updated: %+v", it)
	}
}

func TestAnIdentityTheServiceRefusesOnAReplyIsRetired(t *testing.T) {
	svc, rs := replySetup(t)
	rs.answer = func(_ int, w http.ResponseWriter, _ *http.Request) {
		stubRefuse(w, http.StatusUnauthorized, "feedback.bad_token")
	}
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi"); !errors.Is(err, ErrNotReplyable) || len(rs.hits) != 1 {
		t.Fatalf("a retired identity asked the service again: %v (%d hits)", err, len(rs.hits))
	}
}

func TestSeenTakesWhatWasShownAndReplyMarksNothing(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	_, _ = svc.ListMine(context.Background())
	if err := svc.MarkSeen("FB-7K3M-9QX2", 7); err != nil {
		t.Fatal(err)
	}
	if it, _ := svc.Item("FB-7K3M-9QX2"); it.UnreadReplies != 1 {
		t.Fatalf("a mark up to 7 cleared reply 9: %+v", it)
	}
	if err := svc.MarkSeen("FB-7K3M-9QX2", 3); err != nil {
		t.Fatal(err)
	}
	if it, _ := svc.Item("FB-7K3M-9QX2"); it.UnreadReplies != 1 {
		t.Fatalf("a mark moved backwards: %+v", it)
	}
	var inv *InvalidError
	if err := svc.MarkSeen("FB-7K3M-9QX2", 0); !errors.As(err, &inv) || inv.Field != FieldReplyID {
		t.Fatalf("a mark up to nothing: %v", err)
	}
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi"); err != nil {
		t.Fatal(err)
	}
	if it, _ := svc.Item("FB-7K3M-9QX2"); it.UnreadReplies != 1 {
		t.Fatalf("replying marked the thread seen: %+v", it)
	}
}

func TestThreadTextStoredOnDiskIsCleanedOnReadToo(t *testing.T) {
	svc, _ := replySetup(t)
	_ = svc.store.update(func(st *state) error {
		st.Items[0].Replies = []Reply{{ID: 1, Author: AuthorMaintainer, Body: "a\u001b[31mb\u061cc\u2028d\U000E0041e"}}
		return nil
	})
	it, _ := svc.Item("FB-7K3M-9QX2")
	if it.Replies[0].Body != "abcde" {
		t.Fatalf("body = %q", it.Replies[0].Body)
	}
}

func TestASecondReplyWhileTheFirstIsInFlightIsRefused(t *testing.T) {
	svc, rs := replySetup(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	rs.answer = func(_ int, w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"replyId":5}`)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.Reply(ctx, "FB-7K3M-9QX2", "one"); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first reply never reached the service")
	}
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "one"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second reply: %v", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first reply never finished")
	}
	if _, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "two"); err != nil {
		t.Fatalf("after the first finished: %v", err)
	}
}

func TestSeenCannotClaimRepliesBeyondTheNewestOneHeld(t *testing.T) {
	svc, rs := replySetup(t)
	rs.mineBody = threadMine
	_, _ = svc.ListMine(context.Background())
	if err := svc.MarkSeen("FB-7K3M-9QX2", 1000); err != nil {
		t.Fatal(err)
	}
	rs.mineBody = strings.Replace(threadMine, `{"id":9,`, `{"id":12,"author":"maintainer","body":"later","createdAt":"2026-10-02T08:00:00Z"},{"id":9,`, 1)
	got, _ := svc.ListMine(context.Background())
	if got.Items[0].UnreadReplies != 1 {
		t.Fatalf("a mark past the newest held reply masked a later one: %+v", got.Items[0])
	}
}
