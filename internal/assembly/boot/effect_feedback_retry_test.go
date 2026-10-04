package boot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/feedback"
)

func TestEffectFeedbackSubmitRecoversFromTransientResponse(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &browserScriptProvider{}
	kind := "boot-feedback-retry-probe"
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
		n := len(posts)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"receipt":"FB-7K3M-9QX2","status":"received","installToken":"t","createdAt":"2026-09-30T08:00:00Z"}`)
	}))
	defer svc.Close()
	t.Setenv("REASONIX_FEEDBACK_URL", svc.URL)

	ctrl, err := Build(context.Background(), Options{FeedbackSurface: feedback.SurfaceTUI})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	got, err := ctrl.SubmitFeedback(context.Background(), feedback.Draft{
		Category: feedback.Bug, Body: "Neutral retry fixture", DisplayName: "tester",
		Env: feedback.EnvContext{Surface: feedback.SurfaceTUI},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Receipt != "FB-7K3M-9QX2" {
		t.Fatalf("receipt = %q", got.Receipt)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 2 {
		t.Fatalf("posts = %d", len(posts))
	}
	if posts[0]["idempotencyKey"] == "" || posts[0]["idempotencyKey"] != posts[1]["idempotencyKey"] {
		t.Fatal("idempotency key changed")
	}
}
