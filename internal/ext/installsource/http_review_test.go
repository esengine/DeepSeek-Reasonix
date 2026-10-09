package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestOversizedManifestExecuteCarriesSourceIdentity(t *testing.T) {
	srv := manifestSizeServer(t, strings.Repeat("x", defaultFetchLimit+1), false)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), HTTPClient: srv.Client(), RequireApprovedPlan: true})
	raw, err := json.Marshal(map[string]string{"source": srv.URL + "/SKILL.md", "kind": "skill"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tl.Execute(context.Background(), raw)
	var refusal tool.Refusal
	if out != "" || !errors.Is(err, ErrSourceUnreadable) || !errors.As(err, &refusal) || refusal.Code != "install.source_unreadable" {
		t.Fatalf("source failure identity: output=%q err=%v refusal=%+v", out, err, refusal)
	}
	if !strings.Contains(err.Error(), "install.source_unreadable") {
		t.Fatalf("rendered error lost its identity: %v", err)
	}
}

func TestFetchTextRejectsPartialResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusPartialContent} {
		client := &http.Client{Transport: manifestSizeRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Range") != "" {
				t.Fatal("manifest fetch requested a range")
			}
			return &http.Response{StatusCode: status, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("complete fixture")), Request: r}, nil
		})}
		tl := &Tool{httpClient: client}
		got, err := tl.fetchText(context.Background(), "https://example.test/SKILL.md")
		if status == http.StatusPartialContent {
			if got != "" || !errors.Is(err, ErrSourceUnreadable) {
				t.Fatalf("partial response accepted: body=%q err=%v", got, err)
			}
		} else if err != nil || got != "complete fixture" {
			t.Fatalf("complete status %d: body=%q err=%v", status, got, err)
		}
	}
}

func TestFetchTextDeadlineStaysBounded(t *testing.T) {
	for _, budget := range []time.Duration{0, time.Hour, time.Second} {
		ctx := context.Background()
		if budget > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, budget)
			defer cancel()
		}
		start := time.Now()
		client := &http.Client{Transport: manifestSizeRoundTrip(func(r *http.Request) (*http.Response, error) {
			deadline, ok := r.Context().Deadline()
			if !ok || deadline.After(start.Add(defaultFetchTimeout+100*time.Millisecond)) {
				t.Errorf("fetch budget %s has unbounded deadline %v", budget, deadline)
			}
			if parent, ok := ctx.Deadline(); ok && deadline.After(parent) {
				t.Errorf("fetch widened caller deadline: got %v want <= %v", deadline, parent)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("fixture")), Request: r}, nil
		})}
		tl := &Tool{httpClient: client}
		if _, err := tl.fetchText(ctx, "https://example.test/SKILL.md"); err != nil {
			t.Fatal(err)
		}
	}
}
