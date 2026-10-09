package serve

import (
	"encoding/json"
	"net/http"
	"testing"
)

const levelMine = `{"profile":{"level":3,"adoptedCount":7,"currentThreshold":6,"nextLevel":4,"nextThreshold":12,"remaining":5,"trustState":"active","trustExpiresAt":"2026-11-01T00:00:00Z","observedAt":"2026-10-07T08:00:00Z","effectiveLimits":{"reportsPerHour":8,"reportsPerDay":25,"repliesPerHour":6}},"items":[]}`

func TestFeedbackMineCarriesTheProfileOverHTTP(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{mine: levelMine}, true)
	feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"})
	resp, err := http.Get(srv.URL + "/feedback/mine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(got["profile"], &p); err != nil || p["level"] != float64(3) || p["remaining"] != float64(5) {
		t.Fatalf("profile = %s (%v)", got["profile"], err)
	}
	if limits, _ := p["effectiveLimits"].(map[string]any); limits["reportsPerDay"] != float64(25) {
		t.Fatalf("limits = %v", p["effectiveLimits"])
	}
}

func TestFeedbackWithoutAProfileSaysNull(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, true)
	feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"})
	resp, err := http.Get(srv.URL + "/feedback/mine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if string(got["profile"]) != "null" {
		t.Fatalf("profile = %s", got["profile"])
	}
}

func TestFeedbackRefusalsForwardTheTypedWindow(t *testing.T) {
	cases := []struct {
		name, path, upstream, params string
		status                       int
		code                         string
		want                         map[string]any
	}{
		{"hourly", "/feedback", "feedback.rate_limited", `{"limit":"install_hourly","resetsAt":"2026-10-07T09:00:00.000Z","retryAfterSeconds":1800}`, 429, "feedback.rate_limited",
			map[string]any{"limit": "install_hourly", "resetsAt": "2026-10-07T09:00:00Z", "retryAfterSeconds": float64(1800)}},
		{"reply hourly", "/feedback/FB-7K3M-9QX2/reply", "feedback.rate_limited", `{"limit":"reply_hourly","resetsAt":"2026-10-07T09:00:00Z","retryAfterSeconds":60}`, 429, "feedback.rate_limited",
			map[string]any{"limit": "reply_hourly", "resetsAt": "2026-10-07T09:00:00Z", "retryAfterSeconds": float64(60)}},
		{"item cap", "/feedback/FB-7K3M-9QX2/reply", "feedback.reply_limit", `{"limit":"reply_item","resetsAt":null,"retryAfterSeconds":null}`, 429, "feedback.reply_limit",
			map[string]any{"limit": "reply_item"}},
		{"global", "/feedback", "feedback.busy", `{"limit":"global_daily","resetsAt":"2026-10-08T00:00:00Z","retryAfterSeconds":900}`, 503, "feedback.busy",
			map[string]any{"limit": "global_daily", "resetsAt": "2026-10-08T00:00:00Z", "retryAfterSeconds": float64(900)}},
	}
	for _, c := range cases {
		stub := &feedbackStub{}
		srv := feedbackServer(t, stub, true)
		feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"})
		stub.status, stub.body = map[string]int{"feedback.busy": 503}[c.upstream], `{"error":{"code":"`+c.upstream+`","message":"words that must never be matched","params":`+c.params+`}}`
		if stub.status == 0 {
			stub.status = 429
		}
		body := map[string]any{"category": "bug", "body": "y", "displayName": "kim"}
		if c.path != "/feedback" {
			body = map[string]any{"body": "hi"}
		}
		resp, out := feedbackPost(t, srv.URL+c.path, body)
		got := reasonOf(t, out)
		if resp.StatusCode != c.status || got.Code != c.code {
			t.Errorf("%s: %d %s", c.name, resp.StatusCode, out)
			continue
		}
		raw, _ := json.Marshal(got.Params)
		var params map[string]any
		_ = json.Unmarshal(raw, &params)
		for k, v := range c.want {
			if params[k] != v {
				t.Errorf("%s: params[%s] = %v, want %v (all: %s)", c.name, k, params[k], v, raw)
			}
		}
		if c.name == "item cap" && (params["resetsAt"] != nil || params["retryAfterSeconds"] != nil) {
			t.Errorf("item cap carries a reset it does not have: %s", raw)
		}
	}
}
