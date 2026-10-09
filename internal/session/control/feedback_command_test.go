package control

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/feedback"
)

type feedbackRig struct {
	c       *Controller
	mu      sync.Mutex
	texts   []string
	posts   int
	status  int
	mine    string
	respond string
	onPost  func(body string)
}

func newFeedbackRig(t *testing.T, surface feedback.Surface, withService bool) *feedbackRig {
	t.Helper()
	r := &feedbackRig{respond: `{"receipt":"FB-7K3M-9QX2","status":"received","installToken":"t","createdAt":"2026-09-30T08:00:00Z"}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if req.Method == http.MethodPost {
			r.posts++
			if r.onPost != nil {
				raw, _ := io.ReadAll(req.Body)
				r.onPost(string(raw))
			}
		}
		if r.status != 0 {
			w.WriteHeader(r.status)
		}
		if req.Method == http.MethodGet {
			_, _ = io.WriteString(w, r.mine)
			return
		}
		_, _ = io.WriteString(w, r.respond)
	}))
	t.Cleanup(srv.Close)
	opts := Options{Feedback: FeedbackOptions{Surface: surface}, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			r.mu.Lock()
			r.texts = append(r.texts, e.Text)
			r.mu.Unlock()
		}
	})}
	if withService {
		svc, err := feedback.New(feedback.Config{Home: testenv.TempDir(t), Base: srv.URL, HTTP: srv.Client(), Backoff: []time.Duration{}})
		if err != nil {
			t.Fatal(err)
		}
		opts.Feedback.Service = svc
	}
	r.c = New(opts)
	return r
}

func (r *feedbackRig) last(t *testing.T, contains string) string {
	t.Helper()
	deadline := time.Now().Add(testenv.Budget(t))
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, s := range r.texts {
			if strings.Contains(s, contains) {
				r.mu.Unlock()
				return s
			}
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no notice containing %q; got %q", contains, r.texts)
	return ""
}

// edit changes what the stub server answers; the handler goroutine reads these
// fields under mu, so the test must not write them bare.
func (r *feedbackRig) edit(f func(r *feedbackRig)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(r)
}

func (r *feedbackRig) postCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.posts
}

func TestFeedbackCommandNeedsANicknameThenAnExplicitYes(t *testing.T) {
	r := newFeedbackRig(t, feedback.SurfaceTUI, true)
	r.c.Submit("/feedback bug the sidebar forgets me")
	r.last(t, "set a nickname first")
	r.c.Submit("/feedback name kim")
	r.last(t, "nickname set to kim")
	r.c.Submit("/feedback bug the sidebar forgets me")
	if got := r.last(t, "only if we file your report as an issue"); !strings.Contains(got, "kim") || !strings.Contains(got, "--yes") {
		t.Fatalf("notice = %q", got)
	}
	if r.postCount() != 0 {
		t.Fatal("something was sent without --yes")
	}
	r.c.Submit("/feedback bug --yes the sidebar forgets me")
	r.last(t, "Receipt FB-7K3M-9QX2")
	if r.postCount() != 1 {
		t.Fatalf("posts = %d", r.postCount())
	}
}

func TestFeedbackCommandListShowsStatusAndOfflineHonesty(t *testing.T) {
	r := newFeedbackRig(t, feedback.SurfaceTUI, true)
	r.edit(func(r *feedbackRig) {
		r.mine = `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"Sidebar","status":"fixed","issueNumber":11350,"resolvedVersion":"v2.25.0","createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-02T08:00:00Z"},{"receipt":"FB-2H8P-4WD7","category":"idea","titleSnippet":"Export","status":"duplicate","duplicateOf":11302,"createdAt":"2026-09-29T08:00:00Z","updatedAt":"2026-09-29T08:00:00Z"}]}`
	})
	r.c.Submit("/feedback list")
	r.last(t, "no feedback sent")
	r.c.Submit("/feedback name kim")
	r.c.Submit("/feedback bug --yes x")
	r.last(t, "Receipt")
	r.c.Submit("/feedback list")
	got := r.last(t, "fixed in v2.25.0")
	if !strings.Contains(got, "#11350") || !strings.Contains(got, "duplicate of #11302") {
		t.Fatalf("list = %q", got)
	}
}

func TestFeedbackCommandRefusalsAreSaidFromTheirIdentity(t *testing.T) {
	r := newFeedbackRig(t, feedback.SurfaceTUI, true)
	r.c.Submit("/feedback name kim")
	r.edit(func(r *feedbackRig) {
		r.status = http.StatusTooManyRequests
		r.respond = `{"error":{"code":"feedback.rate_limited"}}`
	})
	r.c.Submit("/feedback bug --yes x")
	r.last(t, "too many submissions")
	r.c.Submit("/feedback rant --yes x")
	r.last(t, "category is not acceptable (bad_value)")
}

func TestFeedbackCommandIsUnavailableWithoutASurfaceOrService(t *testing.T) {
	for name, r := range map[string]*feedbackRig{
		"no surface": newFeedbackRig(t, "", true),
		"no service": newFeedbackRig(t, feedback.SurfaceTUI, false),
	} {
		r.c.Submit("/feedback bug --yes x")
		r.last(t, "not available")
		if r.postCount() != 0 {
			t.Errorf("%s: sent anyway", name)
		}
	}
}

func TestFeedbackIsCompletedAsACommand(t *testing.T) {
	items, _ := SlashArgItems("/feedback ", ArgData{})
	labels := []string{}
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, ",") != "bug,idea,question,other,list,show,reply,name" {
		t.Fatalf("labels = %v", labels)
	}
	found := false
	for _, it := range SubmitSlashCommands(i18n.M) {
		found = found || it.Label == "/feedback"
	}
	if !found {
		t.Fatal("/feedback is missing from the built-in catalogue")
	}
}

func TestFeedbackYesOnlyCountsRightAfterTheCategory(t *testing.T) {
	r := newFeedbackRig(t, feedback.SurfaceTUI, true)
	r.c.Submit("/feedback name kim")
	r.last(t, "nickname set")
	r.c.Submit("/feedback bug the flag --yes appears inside my report")
	r.last(t, "Nothing was sent")
	r.c.Submit("/feedback bug please --yes")
	time.Sleep(100 * time.Millisecond)
	if r.postCount() != 0 {
		t.Fatal("a report that mentions --yes was sent without a preview")
	}
}

func TestFeedbackKeepsTheLinesOfAMultiLineReport(t *testing.T) {
	r := newFeedbackRig(t, feedback.SurfaceTUI, true)
	r.c.Submit("/feedback name kim")
	r.last(t, "nickname set")
	var got string
	r.edit(func(r *feedbackRig) { r.onPost = func(body string) { got = body } })
	r.c.Submit("/feedback bug --yes line one\n  indented line two\n\nline four")
	r.last(t, "Receipt")
	var wire string
	r.edit(func(*feedbackRig) { wire = got })
	if !strings.Contains(wire, `line one\n  indented line two\n\nline four`) {
		t.Fatalf("body on the wire = %s", wire)
	}
}

const rigThread = `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"Sidebar","status":"needs_info","needsInput":true,
"replies":[{"id":7,"author":"maintainer","body":"Which OS?\u001b[2J\nAnd which version?","createdAt":"2026-10-01T08:00:00Z"},{"id":8,"author":"user","body":"macOS","createdAt":"2026-10-01T09:00:00Z"}],
"createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-01T09:00:00Z"},
{"receipt":"FB-2H8P-4WD7","category":"idea","titleSnippet":"Export","status":"closed","createdAt":"2026-09-29T08:00:00Z","updatedAt":"2026-09-29T08:00:00Z"},
{"receipt":"FB-9A9A-1B1B","category":"bug","titleSnippet":"Crash","status":"recorded","issueNumber":11350,"createdAt":"2026-09-28T08:00:00Z","updatedAt":"2026-09-28T08:00:00Z"}]}`

func sentRig(t *testing.T) *feedbackRig {
	t.Helper()
	r := newFeedbackRig(t, feedback.SurfaceTUI, true)
	r.c.Submit("/feedback name kim")
	r.last(t, "nickname set")
	r.c.Submit("/feedback bug --yes x")
	r.last(t, "Receipt")
	r.edit(func(r *feedbackRig) { r.mine = rigThread })
	return r
}

func TestFeedbackListMarksNeedsInputAndNewReplies(t *testing.T) {
	r := sentRig(t)
	r.c.Submit("/feedback list")
	got := r.last(t, "needs info")
	for _, want := range []string{"[needs your input]", "[1 new]", "closed", "1 need your attention"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q:\n%s", want, got)
		}
	}
}

func TestFeedbackShowPrintsTheThreadAsPlainTextAndMarksItRead(t *testing.T) {
	r := sentRig(t)
	r.c.Submit("/feedback show fb-7k3m-9qx2")
	got := r.last(t, "maintainer,")
	if strings.ContainsRune(got, 0x1b) || !strings.Contains(got, "Which OS?") || !strings.Contains(got, "And which version?") || !strings.Contains(got, "you,") || !strings.Contains(got, "/feedback reply FB-7K3M-9QX2") {
		t.Fatalf("thread = %q", got)
	}
	r.c.Submit("/feedback list")
	if list := r.last(t, "need your attention"); strings.Contains(list, "[1 new]") {
		t.Fatalf("a shown thread still reads as new: %s", list)
	}
	r.c.Submit("/feedback show FB-NOPE")
	r.last(t, "no feedback with receipt FB-NOPE")
}

func TestFeedbackReplyNeedsAnExplicitYesAndSaysWhenItBecomesPublic(t *testing.T) {
	r := sentRig(t)
	r.c.Submit("/feedback list")
	r.last(t, "FB-9A9A-1B1B")
	before := r.postCount()
	r.c.Submit("/feedback reply FB-9A9A-1B1B macOS 15")
	if got := r.last(t, "Nothing was sent"); !strings.Contains(got, "#11350") || !strings.Contains(got, "publicly") {
		t.Fatalf("notice = %q", got)
	}
	r.c.Submit("/feedback reply FB-9A9A-1B1B please --yes")
	r.last(t, "Nothing was sent")
	if r.postCount() != before {
		t.Fatal("a reply was sent without --yes directly after the receipt")
	}
	r.c.Submit("/feedback reply FB-9A9A-1B1B --yes macOS 15")
	r.last(t, "Reply sent")
	if r.postCount() != before+1 {
		t.Fatalf("posts = %d, want %d", r.postCount(), before+1)
	}
}

func TestFeedbackReplyRefusalsAreSaidFromTheirIdentity(t *testing.T) {
	r := sentRig(t)
	r.c.Submit("/feedback list")
	r.last(t, "FB-7K3M-9QX2")
	for _, c := range []struct {
		status int
		code   string
		says   string
	}{
		{429, "feedback.reply_limit", "reply limit"},
		{409, "feedback.not_replyable", "takes no reply"},
	} {
		r.edit(func(r *feedbackRig) {
			r.status = c.status
			r.respond = `{"error":{"code":"` + c.code + `"}}`
		})
		r.c.Submit("/feedback reply FB-7K3M-9QX2 --yes hello")
		r.last(t, c.says)
	}
	r.c.Submit("/feedback reply")
	r.last(t, "usage:")
}

func TestFeedbackReplyFailuresNeverTellYouToJustRetry(t *testing.T) {
	r := sentRig(t)
	r.c.Submit("/feedback list")
	r.last(t, "FB-7K3M-9QX2")
	for _, c := range []struct {
		status int
		code   string
		says   string
	}{
		{502, "", "may or may not have been sent - check /feedback show FB-7K3M-9QX2"},
		{401, "feedback.bad_token", "send a new report instead"},
	} {
		r.edit(func(r *feedbackRig) {
			r.status = c.status
			r.respond = `{"error":{"code":"` + c.code + `"}}`
		})
		r.c.Submit("/feedback reply FB-7K3M-9QX2 --yes hello")
		r.last(t, c.says)
	}
}
