package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/feedback"
	"reasonix/internal/session/control"
)

type feedbackStub struct {
	mu     sync.Mutex
	posts  []map[string]any
	status int
	body   string
	mine   string
}

func (f *feedbackStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodPost {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		f.posts = append(f.posts, got)
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.body)
		return
	}
	if r.Method == http.MethodGet && f.mine != "" {
		_, _ = io.WriteString(w, f.mine)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/reply") {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"replyId":55,"createdAt":"2026-10-02T09:00:00Z"}`)
		return
	}
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"recorded","issueNumber":11350,"issueUrl":"https://github.com/esengine/DeepSeek-Reasonix/issues/11350","createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-09-30T09:00:00Z"}]}`)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, `{"receipt":"FB-7K3M-9QX2","status":"received","installToken":"tok","createdAt":"2026-09-30T08:00:00Z"}`)
}

func feedbackServer(t *testing.T, stub *feedbackStub, withService bool) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(stub)
	t.Cleanup(upstream.Close)
	opts := control.Options{Runner: fakeRunner{}, WorkspaceRoot: testenv.TempDir(t), Feedback: control.FeedbackOptions{Surface: feedback.SurfaceStudio, ProviderKind: "deepseek"}}
	if withService {
		svc, err := feedback.New(feedback.Config{Home: testenv.TempDir(t), Base: upstream.URL, HTTP: upstream.Client(), Backoff: []time.Duration{}})
		if err != nil {
			t.Fatal(err)
		}
		opts.Feedback.Service = svc
	}
	bc := NewBroadcaster()
	opts.Sink = bc
	srv := httptest.NewServer(operatorHandler(New(control.New(opts), bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func feedbackPost(t *testing.T, url string, body any) (*http.Response, []byte) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func reasonOf(t *testing.T, raw []byte) Reason {
	t.Helper()
	var r Reason
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("not a refusal: %s", raw)
	}
	return r
}

func TestFeedbackEnvStatesSurfaceProviderAndLimits(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, true)
	resp, err := http.Get(srv.URL + "/feedback/env?locale=zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Env         feedback.Env    `json:"env"`
		DisplayName string          `json:"displayName"`
		Limits      feedback.Limits `json:"limits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Env.Surface != "studio" || got.Env.ProviderKind != "deepseek" || got.Env.Locale != "zh-CN" || got.Limits.Images != 3 || got.DisplayName != "" {
		t.Fatalf("env answer = %+v", got)
	}
}

func TestFeedbackSubmitReachesTheServiceRedactedAndReturnsTheReceipt(t *testing.T) {
	stub := &feedbackStub{}
	srv := feedbackServer(t, stub, true)
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	resp, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{
		"idempotencyKey": "k-1", "category": "bug", "body": "boom api_key=sk-abcdefghijklmnopqrstuvwxyz",
		"displayName": "kim", "locale": "en-US",
		"images": []map[string]string{{"name": "a.png", "dataBase64": base64.StdEncoding.EncodeToString(buf.Bytes())}},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	var got feedback.Receipt
	_ = json.Unmarshal(out, &got)
	if got.Receipt != "FB-7K3M-9QX2" || !got.Redacted {
		t.Fatalf("receipt = %+v", got)
	}
	p := stub.posts[0]
	if p["idempotencyKey"] != "k-1" || strings.Contains(p["body"].(string), "sk-abcdef") || len(p["attachments"].([]any)) != 1 {
		t.Fatalf("upstream saw %v", p)
	}
	if env := p["env"].(map[string]any); env["surface"] != "studio" || env["locale"] != "en-US" {
		t.Fatalf("env = %v", env)
	}
}

func TestFeedbackRefusalsCarryTheirOwnCodes(t *testing.T) {
	cases := []struct {
		status   int
		upstream string
		code     string
		want     int
	}{
		{413, "feedback.too_large", "feedback.too_large", 413},
		{429, "feedback.rate_limited", "feedback.rate_limited", 429},
		{400, "feedback.invalid", "feedback.invalid", 400},
		{503, "feedback.disabled", "feedback.disabled", 503},
		{409, "feedback.duplicate", "feedback.duplicate", 409},
		{401, "feedback.bad_token", "feedback.bad_token", 409},
		{502, "", "feedback.unavailable", 502},
		{503, "feedback.busy", "feedback.busy", 503},
		{400, "feedback.image_metadata", "feedback.image_metadata", 400},
		{403, "feedback.challenge_required", "feedback.challenge_required", 403},
	}
	for _, c := range cases {
		stub := &feedbackStub{status: c.status, body: `{"error":{"code":"` + c.upstream + `"}}`}
		srv := feedbackServer(t, stub, true)
		resp, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"})
		if resp.StatusCode != c.want || reasonOf(t, out).Code != c.code {
			t.Errorf("%s: %d %s", c.upstream, resp.StatusCode, out)
		}
	}
}

func TestFeedbackClientSideRefusalsNameTheField(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, true)
	resp, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": " ", "displayName": "kim"})
	r := reasonOf(t, out)
	if resp.StatusCode != http.StatusBadRequest || r.Code != "feedback.invalid" || r.Params["field"] != "body" || r.Params["reason"] != "empty" {
		t.Fatalf("empty body: %d %s", resp.StatusCode, out)
	}
	_, out = feedbackPost(t, srv.URL+"/feedback", map[string]any{
		"category": "bug", "body": "x", "displayName": "kim",
		"images": []map[string]string{{"name": "a.png", "dataBase64": "!!not base64!!"}},
	})
	if r := reasonOf(t, out); r.Code != "feedback.invalid" || r.Params["field"] != "images" || r.Params["reason"] != "undecodable" {
		t.Fatalf("bad base64: %s", out)
	}
	_, out = feedbackPost(t, srv.URL+"/feedback", map[string]any{
		"category": "bug", "body": "x", "displayName": "kim",
		"images": []map[string]string{{"name": "a.txt", "dataBase64": base64.StdEncoding.EncodeToString([]byte("plain text"))}},
	})
	if r := reasonOf(t, out); r.Params["reason"] != "format" {
		t.Fatalf("text as image: %s", out)
	}
}

func TestFeedbackOversizedBodyIsTooLargeNotBadBody(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, true)
	big := `{"category":"bug","body":"` + strings.Repeat("a", uploadCeiling+10) + `","displayName":"k"}`
	resp, err := http.Post(srv.URL+"/feedback", "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || reasonOf(t, out).Code != "feedback.too_large" {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
}

func TestFeedbackMalformedBodyIsBadBody(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, true)
	resp, err := http.Post(srv.URL+"/feedback", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || reasonOf(t, out).Code != codeBadBody {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
}

func TestFeedbackMineAndNameRoundTrip(t *testing.T) {
	stub := &feedbackStub{}
	srv := feedbackServer(t, stub, true)
	if resp, out := feedbackPost(t, srv.URL+"/feedback/name", map[string]string{"displayName": "  ada "}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("name: %d %s", resp.StatusCode, out)
	}
	if _, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "idea", "body": "x", "displayName": "ada"}); !strings.Contains(string(out), "FB-7K3M-9QX2") {
		t.Fatalf("submit: %s", out)
	}
	resp, err := http.Get(srv.URL + "/feedback/mine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got feedback.Mine
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Offline || len(got.Items) != 1 || got.Items[0].Status != feedback.StatusRecorded || *got.Items[0].IssueNumber != 11350 {
		t.Fatalf("mine = %+v", got)
	}
	resp2, err := http.Get(srv.URL + "/feedback/env")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var env struct {
		DisplayName string `json:"displayName"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&env)
	if env.DisplayName != "ada" {
		t.Fatalf("remembered name = %q", env.DisplayName)
	}
}

func TestFeedbackMineOfflineIsAnAnswerNotAnError(t *testing.T) {
	stub := &feedbackStub{}
	upstream := httptest.NewServer(stub)
	svc, _ := feedback.New(feedback.Config{Home: testenv.TempDir(t), Base: upstream.URL, HTTP: upstream.Client(), Backoff: []time.Duration{}})
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: testenv.TempDir(t), Feedback: control.FeedbackOptions{Service: svc, Surface: feedback.SurfaceStudio}})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()
	if _, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "k"}); !strings.Contains(string(out), "FB-") {
		t.Fatalf("submit: %s", out)
	}
	upstream.Close()
	resp, err := http.Get(srv.URL + "/feedback/mine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got feedback.Mine
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != http.StatusOK || !got.Offline || len(got.Items) != 1 {
		t.Fatalf("%d %+v", resp.StatusCode, got)
	}
	_, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "y", "displayName": "k"})
	if reasonOf(t, out).Code != "feedback.offline" {
		t.Fatalf("submit offline: %s", out)
	}
}

func TestFeedbackWithoutAServiceIsDisabled(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, false)
	resp, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "k"})
	if resp.StatusCode != http.StatusServiceUnavailable || reasonOf(t, out).Code != "feedback.disabled" {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
}

const feedbackThread = `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"x","status":"needs_info","needsInput":true,
"replies":[{"id":7,"author":"maintainer","body":"Which OS?","createdAt":"2026-10-01T08:00:00Z"}],
"createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-01T08:00:00Z"}]}`

func feedbackGetMine(t *testing.T, url string) feedback.Mine {
	t.Helper()
	resp, err := http.Get(url + "/feedback/mine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got feedback.Mine
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestFeedbackThreadReplyAndSeenRoundTrip(t *testing.T) {
	stub := &feedbackStub{mine: feedbackThread}
	srv := feedbackServer(t, stub, true)
	if _, out := feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"}); !strings.Contains(string(out), "FB-7K3M-9QX2") {
		t.Fatalf("submit: %s", out)
	}
	got := feedbackGetMine(t, srv.URL)
	if !got.HasNew || got.Unread != 1 || !got.Items[0].NeedsInput || got.Items[0].UnreadReplies != 1 || len(got.Items[0].Replies) != 1 {
		t.Fatalf("mine = %+v", got)
	}
	if resp, out := feedbackPost(t, srv.URL+"/feedback/FB-7K3M-9QX2/seen", map[string]int{"upTo": 7}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("seen: %d %s", resp.StatusCode, out)
	}
	if got := feedbackGetMine(t, srv.URL); got.Items[0].UnreadReplies != 0 || got.Unread != 1 {
		t.Fatalf("after seen, mine = %+v", got)
	}
	resp, out := feedbackPost(t, srv.URL+"/feedback/FB-7K3M-9QX2/reply", map[string]string{"body": "macOS 15"})
	var rep feedback.ReplyReceipt
	_ = json.Unmarshal(out, &rep)
	if resp.StatusCode != http.StatusOK || rep.ReplyID != 55 {
		t.Fatalf("reply: %d %s", resp.StatusCode, out)
	}
	stub.mu.Lock()
	last := stub.posts[len(stub.posts)-1]
	stub.mu.Unlock()
	if last["body"] != "macOS 15" {
		t.Fatalf("upstream saw %v", last)
	}
}

func TestFeedbackReplyRefusalsCarryTheirOwnCodes(t *testing.T) {
	cases := []struct {
		status   int
		upstream string
		code     string
		want     int
	}{
		{429, "feedback.reply_limit", "feedback.reply_limit", 429},
		{409, "feedback.not_replyable", "feedback.not_replyable", 409},
		{429, "feedback.rate_limited", "feedback.rate_limited", 429},
		{401, "feedback.bad_token", "feedback.bad_token", 409},
		{502, "", "feedback.unavailable", 502},
	}
	for _, c := range cases {
		stub := &feedbackStub{}
		srv := feedbackServer(t, stub, true)
		feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"})
		stub.status, stub.body = c.status, `{"error":{"code":"`+c.upstream+`"}}`
		resp, out := feedbackPost(t, srv.URL+"/feedback/FB-7K3M-9QX2/reply", map[string]string{"body": "hi"})
		if resp.StatusCode != c.want || reasonOf(t, out).Code != c.code {
			t.Errorf("%s: %d %s", c.upstream, resp.StatusCode, out)
		}
	}
}

func TestFeedbackReplyRefusesLocallyAndWithoutAService(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{}, true)
	for _, c := range []struct {
		path, body, field string
	}{
		{"/feedback/FB-7K3M-9QX2/reply", `{"body":"hi"}`, "receipt"},
		{"/feedback/FB-7K3M-9QX2/reply", `{"body":"  "}`, "body"},
	} {
		resp, err := http.Post(srv.URL+c.path, "application/json", strings.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		r := reasonOf(t, raw)
		if resp.StatusCode != http.StatusBadRequest || r.Code != "feedback.invalid" || r.Params["field"] != c.field {
			t.Errorf("%s: %d %s", c.body, resp.StatusCode, raw)
		}
	}
	bare := feedbackServer(t, &feedbackStub{}, false)
	if resp, out := feedbackPost(t, bare.URL+"/feedback/FB-7K3M-9QX2/reply", map[string]string{"body": "hi"}); reasonOf(t, out).Code != "feedback.disabled" {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
	if resp, out := feedbackPost(t, bare.URL+"/feedback/FB-7K3M-9QX2/seen", map[string]int{"upTo": 7}); reasonOf(t, out).Code != "feedback.disabled" {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
}

func TestFeedbackSubmitForwardsTheChallengeToken(t *testing.T) {
	stub := &feedbackStub{}
	srv := feedbackServer(t, stub, true)
	feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim", "turnstileToken": "tok-abc"})
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.posts) != 1 || stub.posts[0]["turnstileToken"] != "tok-abc" {
		t.Fatalf("upstream saw %v", stub.posts)
	}
}

func TestFeedbackSeenNeedsTheReplyItWasShownUpTo(t *testing.T) {
	srv := feedbackServer(t, &feedbackStub{mine: feedbackThread}, true)
	feedbackPost(t, srv.URL+"/feedback", map[string]any{"category": "bug", "body": "x", "displayName": "kim"})
	resp, out := feedbackPost(t, srv.URL+"/feedback/FB-7K3M-9QX2/seen", map[string]int{})
	r := reasonOf(t, out)
	if resp.StatusCode != http.StatusBadRequest || r.Code != "feedback.invalid" || r.Params["field"] != "replyId" {
		t.Fatalf("%d %s", resp.StatusCode, out)
	}
	feedbackGetMine(t, srv.URL)
	feedbackPost(t, srv.URL+"/feedback/FB-7K3M-9QX2/seen", map[string]int{"upTo": 3})
	if got := feedbackGetMine(t, srv.URL); got.Items[0].UnreadReplies != 1 {
		t.Fatalf("a mark up to 3 cleared reply 7: %+v", got.Items[0])
	}
}
