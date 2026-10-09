package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSubmitTransientResponseRetry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		recover bool
		want    error
		count   int
	}{
		{"gateway recovers", 503, "gateway", true, nil, 2},
		{"gateway persists", 503, "gateway", false, ErrUnavailable, 2},
		{"undecodable recovers", 200, "not JSON", true, nil, 2},
		{"undecodable persists", 200, "not JSON", false, ErrUnavailable, 2},
		{"rate limited", 429, `{"error":{"code":"feedback.rate_limited"}}`, false, ErrRateLimited, 1},
		{"disabled", 403, `{"error":{"code":"feedback.disabled"}}`, false, ErrDisabled, 1},
		{"coded busy gateway", 503, `{"error":{"code":"feedback.busy"}}`, false, ErrBusy, 1},
		{"uncoded forbidden", 403, "unknown", false, ErrUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var keys []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b wireSubmit
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
					t.Error(err)
				}
				keys = append(keys, b.IdempotencyKey)
				if tc.recover && len(keys) > 1 {
					_, _ = io.WriteString(w, `{"receipt":"FB-7K3M-9QX2","status":"received"}`)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			svc, err := New(Config{Home: t.TempDir(), Base: server.URL, Backoff: []time.Duration{time.Millisecond, time.Millisecond}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.post(context.Background(), wireSubmit{IdempotencyKey: "neutral-key"}, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.recover && got.Receipt != "FB-7K3M-9QX2" {
				t.Fatalf("receipt = %q", got.Receipt)
			}
			if len(keys) != tc.count {
				t.Fatalf("requests = %d, want %d", len(keys), tc.count)
			}
			for _, key := range keys {
				if key != "neutral-key" {
					t.Fatalf("key = %q", key)
				}
			}
		})
	}
}

func TestTransientResponseDoesNotRetryReplyOrRead(t *testing.T) {
	for _, operation := range []string{"reply", "read"} {
		t.Run(operation, func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count++; w.WriteHeader(http.StatusServiceUnavailable) }))
			defer server.Close()
			svc, _ := New(Config{Home: t.TempDir(), Base: server.URL, Backoff: []time.Duration{time.Millisecond}})
			var err error
			if operation == "reply" {
				_, err = svc.postReply(context.Background(), "install", "token", "FB-7K3M-9QX2", "neutral reply")
			} else {
				err = svc.get(context.Background(), "install", "token", new(any))
			}
			if !errors.Is(err, ErrUnavailable) || count != 1 {
				t.Fatalf("error = %v; requests = %d", err, count)
			}
		})
	}
}

func TestSubmitRedirectRefusalIsNotRetried(t *testing.T) {
	count, targets := 0, 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targets++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	svc, _ := New(Config{Home: t.TempDir(), Base: server.URL, Backoff: []time.Duration{time.Millisecond}})
	_, err := svc.post(context.Background(), wireSubmit{IdempotencyKey: "neutral-key"}, "")
	if !errors.Is(err, ErrUnavailable) || count != 1 || targets != 0 {
		t.Fatalf("error = %v; requests = %d; targets = %d", err, count, targets)
	}
}

func TestSubmitTransientBackoffCancellation(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count++; w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	svc, _ := New(Config{Home: t.TempDir(), Base: server.URL, Backoff: []time.Duration{time.Hour}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := svc.withRetry(ctx, func() error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
		err := svc.do(req, new(any))
		timer := time.AfterFunc(20*time.Millisecond, cancel)
		t.Cleanup(func() { timer.Stop() })
		return err
	}, true)
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("error = %v; requests = %d", err, count)
	}
}
