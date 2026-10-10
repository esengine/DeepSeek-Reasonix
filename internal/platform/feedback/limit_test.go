package feedback

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func refusal(status int, code, params string, retryAfter string) func(int, http.ResponseWriter, *http.Request) bool {
	return func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"code":"`+code+`","message":"words that must never be matched"`+params+`}}`)
		return true
	}
}

func TestRateLimitDecodesTheTypedWindow(t *testing.T) {
	st := &stub{postFn: refusal(429, "feedback.rate_limited", `,"params":{"limit":"install_daily","resetsAt":"2026-10-08T00:00:00.000Z","retryAfterSeconds":5400}`, "5400")}
	svc, _ := setup(t, st)
	_, err := svc.Submit(context.Background(), draft())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	got, ok := LimitOf(err)
	want := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if !ok || got.Limit != LimitInstallDaily || !got.ResetsAt.Equal(want) || got.After != 90*time.Minute {
		t.Fatalf("limit = %+v %v", got, ok)
	}
	if RetryAfter(err) != 90*time.Minute {
		t.Fatalf("retry after = %v", RetryAfter(err))
	}
}

func TestReplyWindowAndPermanentItemCapAreTypedApart(t *testing.T) {
	cases := []struct {
		name, code, params string
		sentinel           error
		limit              Limit
		resets             bool
	}{
		{"reply hourly", "feedback.rate_limited", `,"params":{"limit":"reply_hourly","resetsAt":"2026-10-07T13:00:00Z","retryAfterSeconds":60}`, ErrRateLimited, LimitReplyHourly, true},
		{"item cap", "feedback.reply_limit", `,"params":{"limit":"reply_item","resetsAt":null,"retryAfterSeconds":null}`, ErrReplyLimit, LimitReplyItem, false},
		{"global", "feedback.busy", `,"params":{"limit":"global_daily","resetsAt":"2026-10-08T00:00:00Z","retryAfterSeconds":3000}`, ErrBusy, LimitGlobalDaily, true},
		{"unknown window", "feedback.rate_limited", `,"params":{"limit":"something_new","resetsAt":"not a time","retryAfterSeconds":"soon"}`, ErrRateLimited, Limit("something_new"), false},
	}
	for _, c := range cases {
		st := &stub{postFn: refusal(429, c.code, c.params, "")}
		svc, _ := setup(t, st)
		_, err := svc.Submit(context.Background(), draft())
		if !errors.Is(err, c.sentinel) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.sentinel)
			continue
		}
		got, ok := LimitOf(err)
		if !ok || got.Limit != c.limit || got.ResetsAt.IsZero() == c.resets {
			t.Errorf("%s: limit = %+v %v", c.name, got, ok)
		}
	}
}

func TestACodedRefusalWithoutParamsStillCarriesItsSentinelAndHeader(t *testing.T) {
	st := &stub{postFn: refusal(429, "feedback.rate_limited", "", "30")}
	svc, _ := setup(t, st)
	_, err := svc.Submit(context.Background(), draft())
	if !errors.Is(err, ErrRateLimited) || RetryAfter(err) != 30*time.Second {
		t.Fatalf("err = %v after %v", err, RetryAfter(err))
	}
	if got, ok := LimitOf(err); !ok || got.Limit != "" || !got.ResetsAt.IsZero() {
		t.Fatalf("an old worker names no window: %+v %v", got, ok)
	}
	if _, ok := LimitOf(ErrOffline); ok {
		t.Fatal("an offline error carries no window")
	}
}

func TestLimitedReplyKeepsItsSentinelThroughThePostReplyPath(t *testing.T) {
	svc, rs := replySetup(t)
	rs.answer = func(n int, w http.ResponseWriter, r *http.Request) {
		refusal(429, "feedback.reply_limit", `,"params":{"limit":"reply_item","resetsAt":null,"retryAfterSeconds":null}`, "")(n, w, r)
	}
	_, err := svc.Reply(context.Background(), "FB-7K3M-9QX2", "hi")
	if !errors.Is(err, ErrReplyLimit) {
		t.Fatalf("err = %v", err)
	}
	if got, ok := LimitOf(err); !ok || got.Limit != LimitReplyItem {
		t.Fatalf("limit = %+v %v", got, ok)
	}
}
