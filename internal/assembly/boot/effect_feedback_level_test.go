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

// Through the real assembly: the standing the service states reaches the
// controller's list and the terminal's notice, a refusal that names its window
// reaches the notice with that window, and neither ever reaches a model request.
func TestEffectFeedbackStandingAndTypedRefusalReachTheFrontendNotTheModel(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &browserScriptProvider{}
	kind := "boot-feedback-level-probe"
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
	refuse := false
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		refusing := refuse
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"profile":{"level":2,"adoptedCount":4,"currentThreshold":3,"nextLevel":3,"nextThreshold":6,"remaining":2,"trustState":"active","trustExpiresAt":"2026-11-01T00:00:00Z","observedAt":"2026-10-07T08:00:00Z","effectiveLimits":{"reportsPerHour":6,"reportsPerDay":20,"repliesPerHour":5}},"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"fixed","resolvedVersion":"v2.25.0","createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-02T08:00:00Z"}]}`)
		case refusing:
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"feedback.rate_limited","message":"submission limit reached","params":{"limit":"install_daily","resetsAt":"2026-10-08T00:00:00Z","retryAfterSeconds":3600}}}`)
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
	waitNotice := func(contains string) string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			nmu.Lock()
			for _, n := range notices {
				if strings.Contains(n, contains) {
					nmu.Unlock()
					return n
				}
			}
			nmu.Unlock()
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

	mine, err := ctrl.ListFeedback(context.Background())
	if err != nil || mine.Profile == nil || mine.Profile.Level != 2 || mine.Profile.Remaining == nil || *mine.Profile.Remaining != 2 || mine.Profile.EffectiveLimits.ReportsPerHour != 6 {
		t.Fatalf("the controller's list = %+v %v", mine, err)
	}
	ctrl.Submit("/feedback list")
	if got := waitNotice("Seedling"); !strings.Contains(got, "2 more to reach Sapling") || !strings.Contains(got, "6 reports an hour") {
		t.Fatalf("standing notice = %q", got)
	}

	mu.Lock()
	refuse = true
	mu.Unlock()
	ctrl.Submit("/feedback bug --yes again")
	if got := waitNotice("the daily report limit was reached"); strings.Contains(got, "too many submissions") {
		t.Fatalf("refusal notice = %q", got)
	}
	if err := ctrl.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	reqs := rec.requests()
	if len(reqs) != 2 {
		t.Fatalf("the model saw %d requests: /feedback must not start a turn", len(reqs))
	}
	for i, r := range reqs {
		raw, _ := json.Marshal(r)
		for _, leak := range []string{"Seedling", "Sapling", "install_daily", "shipped", "FB-7K3M"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(leak)) {
				t.Errorf("request %d carries %q", i, leak)
			}
		}
	}
	if !reflect.DeepEqual(reqs[0].Tools, reqs[1].Tools) {
		t.Error("the tool schema moved between the turns around the standing")
	}
	if n := len(reqs[0].Messages); len(reqs[1].Messages) < n || !reflect.DeepEqual(reqs[1].Messages[:n], reqs[0].Messages) {
		t.Error("the message prefix moved between the turns around the standing")
	}
}
