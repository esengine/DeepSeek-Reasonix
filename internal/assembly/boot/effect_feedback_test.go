package boot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/feedback"
)

// Through the real assembly: sending feedback is a host action. What the
// person typed goes to the feedback service redacted, and nothing about it —
// not the text, not a notice, not a tool — ever reaches a model request, so
// the cache-stable prefix is the one a session without feedback would send.
func TestEffectFeedbackNeverReachesTheModelOrMovesThePrefix(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &browserScriptProvider{}
	kind := "boot-feedback-probe"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)

	var mu sync.Mutex
	var posts []map[string]any
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		mu.Lock()
		posts = append(posts, got)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"receipt":"FB-7K3M-9QX2","status":"received","installToken":"t","createdAt":"2026-09-30T08:00:00Z"}`)
	}))
	defer svc.Close()
	t.Setenv("REASONIX_FEEDBACK_URL", svc.URL)

	var notices []string
	var nmu sync.Mutex
	ctrl, err := Build(context.Background(), Options{
		FeedbackSurface: feedback.SurfaceTUI,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				nmu.Lock()
				notices = append(notices, e.Text)
				nmu.Unlock()
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SetFeedbackDisplayName("kim"); err != nil {
		t.Fatal(err)
	}
	const marker = "MARKER-the-sidebar-forgets-me"
	ctrl.Submit("/feedback bug --yes " + marker + " api_key=sk-abcdefghijklmnopqrstuvwxyz")
	deadline := time.Now().Add(10 * time.Second)
	for {
		nmu.Lock()
		done := strings.Contains(strings.Join(notices, "\n"), "Receipt FB-7K3M-9QX2")
		nmu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no receipt notice; notices = %q", notices)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := ctrl.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 1 {
		t.Fatalf("the service saw %d posts", len(posts))
	}
	sent := posts[0]
	if body := sent["body"].(string); !strings.Contains(body, marker) || strings.Contains(body, "sk-abcdef") {
		t.Fatalf("body on the wire = %q", body)
	}
	if env := sent["env"].(map[string]any); env["surface"] != "tui" || env["providerKind"] != "other" {
		t.Fatalf("env on the wire = %v", env)
	}

	reqs := rec.requests()
	if len(reqs) != 2 {
		t.Fatalf("the model saw %d requests: /feedback must not start a turn", len(reqs))
	}
	for i, r := range reqs {
		raw, _ := json.Marshal(r)
		for _, leak := range []string{marker, "FB-7K3M", "sk-abcdef"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(leak)) {
				t.Errorf("request %d carries %q", i, leak)
			}
		}
	}
	if !reflect.DeepEqual(reqs[0].Tools, reqs[1].Tools) {
		t.Error("the tool schema moved between the turns around a /feedback")
	}
	if n := len(reqs[0].Messages); len(reqs[1].Messages) < n || !reflect.DeepEqual(reqs[1].Messages[:n], reqs[0].Messages) {
		t.Error("the message prefix moved between the turns around a /feedback")
	}
}

// A maintainer's reply, the person's answer and the read state are host data:
// they go to the feedback service and to the frontend, and never into a model
// request, so the prefix and the tool schema stay what a session without the
// thread would send.
func TestEffectFeedbackThreadNeverReachesTheModel(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &browserScriptProvider{}
	kind := "boot-feedback-thread-probe"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)

	const asked = "MARKER-which-os-are-you-on"
	var mu sync.Mutex
	var replies []string
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"needs_info","needsInput":true,"replies":[{"id":7,"author":"maintainer","body":"`+asked+`","createdAt":"2026-10-01T08:00:00Z"}],"createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-01T08:00:00Z"}]}`)
		case strings.HasSuffix(r.URL.Path, "/reply"):
			var got map[string]any
			_ = json.NewDecoder(r.Body).Decode(&got)
			mu.Lock()
			replies = append(replies, got["body"].(string))
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"replyId":9,"createdAt":"2026-10-02T08:00:00Z"}`)
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"receipt":"FB-7K3M-9QX2","status":"received","installToken":"t","createdAt":"2026-09-30T08:00:00Z"}`)
		}
	}))
	defer svc.Close()
	t.Setenv("REASONIX_FEEDBACK_URL", svc.URL)

	var notices []string
	var nmu sync.Mutex
	ctrl, err := Build(context.Background(), Options{
		FeedbackSurface: feedback.SurfaceTUI,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				nmu.Lock()
				notices = append(notices, e.Text)
				nmu.Unlock()
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	waitNotice := func(contains string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			nmu.Lock()
			done := strings.Contains(strings.Join(notices, "\n"), contains)
			nmu.Unlock()
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("no notice containing %q; notices = %q", contains, notices)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	if err := ctrl.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SetFeedbackDisplayName("kim"); err != nil {
		t.Fatal(err)
	}
	ctrl.Submit("/feedback bug --yes x")
	waitNotice("Receipt FB-7K3M-9QX2")
	ctrl.Submit("/feedback show FB-7K3M-9QX2")
	waitNotice(asked)
	const answer = "MARKER-macos-fifteen"
	ctrl.Submit("/feedback reply FB-7K3M-9QX2 --yes " + answer)
	waitNotice("Reply sent")
	if err := ctrl.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	if len(replies) != 1 || replies[0] != answer {
		t.Fatalf("the service saw replies %q", replies)
	}
	mu.Unlock()
	reqs := rec.requests()
	if len(reqs) != 2 {
		t.Fatalf("the model saw %d requests: /feedback must not start a turn", len(reqs))
	}
	for i, r := range reqs {
		raw, _ := json.Marshal(r)
		for _, leak := range []string{asked, answer, "FB-7K3M"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(leak)) {
				t.Errorf("request %d carries %q", i, leak)
			}
		}
	}
	if !reflect.DeepEqual(reqs[0].Tools, reqs[1].Tools) {
		t.Error("the tool schema moved between the turns around a thread")
	}
	if n := len(reqs[0].Messages); len(reqs[1].Messages) < n || !reflect.DeepEqual(reqs[1].Messages[:n], reqs[0].Messages) {
		t.Error("the message prefix moved between the turns around a thread")
	}
}
