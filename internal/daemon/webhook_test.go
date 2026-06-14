package daemon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestWebhookValidSignature(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-test.jsonl")
	os.WriteFile(sess, []byte(`{}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-test",
		Goal:      agent.RuntimeGoalMeta{Text: "deploy", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})

	secret := "test-secret-key"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	// Set up HTTP server.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", d.handleWebhook)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	addr := ln.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second}

	// Build a valid webhook payload.
	payload := `{"type":"github.ci","session_id":"webhook-test","summary":"CI passed","event_id":"evt-001"}`
	sig := computeHMAC([]byte(payload), secret)

	req, _ := http.NewRequest("POST", "http://"+addr+"/webhook", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", sig)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Verify the session was updated.
	d.mu.RLock()
	entry := d.registry["webhook-test"]
	d.mu.RUnlock()
	if entry.Runtime.Run.Status != "queued" {
		t.Errorf("Run.Status = %q, want queued", entry.Runtime.Run.Status)
	}
	if entry.Runtime.Scheduler.LastWakeupEventID != "evt-001" {
		t.Errorf("LastWakeupEventID = %q, want evt-001", entry.Runtime.Scheduler.LastWakeupEventID)
	}
}

func TestWebhookStoresRawPayloadReferenceOutsidePromptContext(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-payload-ref.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-payload-ref",
		Goal:      agent.RuntimeGoalMeta{Text: "deploy", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "payload-ref-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"type":"github.ci","session_id":"webhook-payload-ref","summary":"CI passed","event_id":"evt-payload-ref","payload":{"raw_secret":"do-not-inject"}}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case intent := <-d.intentCh:
		if strings.Contains(intent.Context, "do-not-inject") {
			t.Fatalf("raw payload leaked into bounded context:\n%s", intent.Context)
		}
	default:
		t.Fatal("webhook did not enqueue an intent")
	}

	events, ok, err := agent.LoadRuntimeTimeline(sess, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: err=%v ok=%v", err, ok)
	}
	var payloadRef string
	for _, event := range events {
		if event.Type == "webhook_event_received" {
			payloadRef = event.PayloadRef
			break
		}
	}
	if payloadRef == "" {
		t.Fatalf("webhook timeline event missing payload ref: %+v", events)
	}
	payloadPath, ok := webhookPayloadPathFromRef(sess, payloadRef)
	if !ok {
		t.Fatalf("invalid payload ref %q", payloadRef)
	}
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", payloadPath, err)
	}
	if string(raw) != payload {
		t.Fatalf("stored payload mismatch:\ngot  %s\nwant %s", raw, payload)
	}
	info, err := os.Stat(payloadPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", payloadPath, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("payload file mode = %o, want 600", got)
	}
}

func TestWebhookInvalidSignature(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "sig-test.jsonl")
	os.WriteFile(sess, []byte(`{}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "sig-test",
		Goal:      agent.RuntimeGoalMeta{Text: "work", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})

	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: "correct-secret", Enabled: true},
	})
	d.scanSessions()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", d.handleWebhook)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	addr := ln.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second}

	payload := `{"type":"test","session_id":"sig-test","summary":"bad"}`
	badSig := computeHMAC([]byte(payload), "wrong-secret")

	req, _ := http.NewRequest("POST", "http://"+addr+"/webhook", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", badSig)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWebhookDedup(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "dedup-wh.jsonl")
	os.WriteFile(sess, []byte(`{}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "dedup-wh",
		Goal:      agent.RuntimeGoalMeta{Text: "work", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})

	secret := "dedup-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", d.handleWebhook)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	addr := ln.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second}

	payload := `{"type":"github.ci","session_id":"dedup-wh","summary":"CI done","event_id":"evt-dup"}`
	sig := computeHMAC([]byte(payload), secret)

	// First request.
	req1, _ := http.NewRequest("POST", "http://"+addr+"/webhook", strings.NewReader(payload))
	req1.Header.Set("X-Webhook-Signature", sig)
	resp1, _ := client.Do(req1)
	resp1.Body.Close()

	// Second request with same event_id.
	req2, _ := http.NewRequest("POST", "http://"+addr+"/webhook", strings.NewReader(payload))
	req2.Header.Set("X-Webhook-Signature", sig)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("second POST /webhook: %v", err)
	}
	defer resp2.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&result)
	if result["status"] != "duplicate" {
		t.Errorf("second request should be deduped, got status=%v", result["status"])
	}
}

func TestWebhookRoutesGitHubEventWithoutSessionID(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "github-route.jsonl")
	os.WriteFile(sess, []byte(`{}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "github-route",
		Goal:      agent.RuntimeGoalMeta{Text: "watch esengine/DeepSeek-Reasonix PR #42", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})

	secret := "route-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"action":"completed","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"workflow_run":{"status":"completed","conclusion":"success","pull_requests":[{"number":42}],"head_branch":"feature/agentos"}}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-GitHub-Delivery", "delivery-42")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	d.mu.RLock()
	entry := d.registry["github-route"]
	d.mu.RUnlock()
	if entry.Runtime.Run.Status != "queued" {
		t.Fatalf("Run.Status = %q, want queued", entry.Runtime.Run.Status)
	}
	if entry.Runtime.Scheduler.LastWakeupEventID != "delivery-42" {
		t.Fatalf("LastWakeupEventID = %q, want delivery-42", entry.Runtime.Scheduler.LastWakeupEventID)
	}
}

func TestWebhookRespectsDailyBudget(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-budget.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	now := time.Now().UTC()
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-budget",
		Goal:      agent.RuntimeGoalMeta{Text: "deploy", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 1,
			DailyWakeups:     1,
			WindowStartedAt:  budgetWindowStart(now),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "budget-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"type":"github.ci","session_id":"webhook-budget","summary":"CI passed","event_id":"evt-budget"}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "budget_blocked" {
		t.Fatalf("status = %v, want budget_blocked", resp["status"])
	}
	select {
	case intent := <-d.intentCh:
		t.Fatalf("budget-blocked webhook should not enqueue intent: %+v", intent)
	default:
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" || loaded.Scheduler.LastWakeupEventID != "evt-budget" || loaded.Budget.LastBlockedReason == "" {
		t.Fatalf("budget block not persisted: run=%+v scheduler=%+v budget=%+v", loaded.Run, loaded.Scheduler, loaded.Budget)
	}
}

func TestWebhookIgnoresNonMatchingWaitEvent(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-wait-ignore.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-wait-ignore",
		Goal:      agent.RuntimeGoalMeta{Text: "wait for CI", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:        "event",
			EventSource: "github.workflow_run",
			Reason:      "waiting for CI",
			Subject:     "PR #42",
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 2,
			WindowStartedAt:  budgetWindowStart(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "wait-ignore-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"action":"opened","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"pull_request":{"number":42,"title":"feat"}}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-pr")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ignored" {
		t.Fatalf("status = %v, want ignored", resp["status"])
	}
	select {
	case intent := <-d.intentCh:
		t.Fatalf("ignored webhook should not enqueue intent: %+v", intent)
	default:
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_event" || loaded.Wait.Kind != "event" {
		t.Fatalf("wait condition should remain active: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	if loaded.Budget.DailyWakeups != 0 {
		t.Fatalf("ignored webhook should not consume budget: %+v", loaded.Budget)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "wait_event_ignored" {
		t.Fatalf("ignored wait timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}
}

func TestWebhookMatchesWaitEventAndClearsWait(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-wait-match.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-wait-match",
		Goal:      agent.RuntimeGoalMeta{Text: "wait for CI", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     "github.workflow_run",
			EventID:         "delivery-42",
			EventStatus:     "completed",
			EventConclusion: "success",
			Reason:          "waiting for CI",
			Subject:         "PR #42",
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 2,
			WindowStartedAt:  budgetWindowStart(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "wait-match-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"action":"completed","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"workflow_run":{"status":"completed","conclusion":"success","pull_requests":[{"number":42}],"head_branch":"feature/agentos"}}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-GitHub-Delivery", "delivery-42")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "queued" {
		t.Fatalf("status = %v, want queued", resp["status"])
	}
	select {
	case intent := <-d.intentCh:
		if intent.Source != "webhook" || intent.Reason != "webhook:github.workflow_run" || intent.EventID != "delivery-42" {
			t.Fatalf("unexpected intent: %+v", intent)
		}
		if !strings.Contains(intent.Context, "workflow_run") || !strings.Contains(intent.Context, "success") {
			t.Fatalf("missing bounded webhook context:\n%s", intent.Context)
		}
	default:
		t.Fatal("matching webhook did not enqueue an intent")
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "queued" || loaded.Wait.Kind != "" {
		t.Fatalf("matching webhook should clear wait and continue: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	if loaded.Scheduler.LastWakeupEventID != "delivery-42" || loaded.Budget.DailyWakeups != 1 {
		t.Fatalf("wakeup metadata not persisted: scheduler=%+v budget=%+v", loaded.Scheduler, loaded.Budget)
	}
}

func TestWebhookMatchesGitHubCheckSuiteWait(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-check-suite.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-check-suite",
		Goal:      agent.RuntimeGoalMeta{Text: "watch esengine/DeepSeek-Reasonix PR #42 checks", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     "github.check_suite",
			EventStatus:     "completed",
			EventConclusion: "success",
			Reason:          "waiting for check suite",
			Subject:         "PR #42",
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 2,
			WindowStartedAt:  budgetWindowStart(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "check-suite-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"action":"completed","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"check_suite":{"status":"completed","conclusion":"success","pull_requests":[{"number":42}],"head_branch":"feature/agentos"}}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "check_suite")
	req.Header.Set("X-GitHub-Delivery", "delivery-check-suite")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["session_id"] != "webhook-check-suite" || resp["status"] != "queued" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	select {
	case intent := <-d.intentCh:
		if intent.Source != "webhook" || intent.Reason != "webhook:github.check_suite" || intent.EventID != "delivery-check-suite" {
			t.Fatalf("unexpected intent: %+v", intent)
		}
		for _, want := range []string{"github.check_suite", "status=completed", "conclusion=success", "number=42", "ref=feature/agentos"} {
			if !strings.Contains(intent.Context, want) {
				t.Fatalf("intent context missing %q:\n%s", want, intent.Context)
			}
		}
	default:
		t.Fatal("matching check_suite webhook did not enqueue an intent")
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "queued" || loaded.Wait.Kind != "" {
		t.Fatalf("check_suite webhook should clear wait and continue: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	if loaded.Scheduler.LastWakeupKey == "" || loaded.Budget.DailyWakeups != 1 {
		t.Fatalf("wakeup metadata not persisted: scheduler=%+v budget=%+v", loaded.Scheduler, loaded.Budget)
	}
}

func TestWebhookMatchesGitHubStatusWait(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-status.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-status",
		Goal:      agent.RuntimeGoalMeta{Text: "watch esengine/DeepSeek-Reasonix branch main status", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     "github.status",
			EventConclusion: "success",
			Reason:          "waiting for commit status",
			Subject:         "branch main",
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 2,
			WindowStartedAt:  budgetWindowStart(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "status-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"state":"success","context":"ci/build","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"branches":[{"name":"main"}]}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "status")
	req.Header.Set("X-GitHub-Delivery", "delivery-status")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["session_id"] != "webhook-status" || resp["status"] != "queued" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	select {
	case intent := <-d.intentCh:
		if intent.Source != "webhook" || intent.Reason != "webhook:github.status" || intent.EventID != "delivery-status" {
			t.Fatalf("unexpected intent: %+v", intent)
		}
		for _, want := range []string{"github.status", "status=success", "conclusion=success", "ref=main", "title=ci/build"} {
			if !strings.Contains(intent.Context, want) {
				t.Fatalf("intent context missing %q:\n%s", want, intent.Context)
			}
		}
	default:
		t.Fatal("matching status webhook did not enqueue an intent")
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "queued" || loaded.Wait.Kind != "" {
		t.Fatalf("status webhook should clear wait and continue: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
}

func TestGitHubWebhookInfoNormalizesPushAndRelease(t *testing.T) {
	pushPayload := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"esengine/DeepSeek-Reasonix"}}`)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(string(pushPayload)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-push")
	info := extractGitHubWebhookInfo(req, pushPayload)
	if info.Event != "push" || info.Action != "push" || info.Ref != "main" || info.Repo != "esengine/DeepSeek-Reasonix" {
		t.Fatalf("unexpected push info: %+v", info)
	}
	if summary := webhookSummary(WebhookEvent{Type: "github.push"}, info); !strings.Contains(summary, "action=push") || !strings.Contains(summary, "ref=main") {
		t.Fatalf("push summary missing normalized fields: %s", summary)
	}

	releasePayload := []byte(`{"action":"published","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"release":{"tag_name":"v1.2.3","name":"Reasonix v1.2.3"}}`)
	req = httptest.NewRequest("POST", "/webhook", strings.NewReader(string(releasePayload)))
	req.Header.Set("X-GitHub-Event", "release")
	req.Header.Set("X-GitHub-Delivery", "delivery-release")
	info = extractGitHubWebhookInfo(req, releasePayload)
	if info.Event != "release" || info.Action != "published" || info.Ref != "v1.2.3" || info.Title != "Reasonix v1.2.3" {
		t.Fatalf("unexpected release info: %+v", info)
	}
	if summary := webhookSummary(WebhookEvent{Type: "github.release"}, info); !strings.Contains(summary, "ref=v1.2.3") || !strings.Contains(summary, "title=Reasonix v1.2.3") {
		t.Fatalf("release summary missing normalized fields: %s", summary)
	}
}

func TestWebhookAdapterRegistryNormalizesNonGitHubProvider(t *testing.T) {
	prev := defaultWebhookAdapters
	defaultWebhookAdapters = webhookAdapterRegistry{
		adapters: append([]WebhookAdapter{testWebhookAdapter{}}, prev.adapters...),
	}
	t.Cleanup(func() { defaultWebhookAdapters = prev })

	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-adapter.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-adapter",
		Goal:      agent.RuntimeGoalMeta{Text: "watch example/repo branch main build", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     "example.build",
			EventConclusion: "success",
			Reason:          "waiting for external build",
			Subject:         "branch main",
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 2,
			WindowStartedAt:  budgetWindowStart(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	webhookKey := "adapter-registry-key"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: webhookKey, Enabled: true},
	})
	d.scanSessions()

	payload := `{"delivery":"adapter-delivery-1","repo":"example/repo","ref":"main","state":"failed","title":"adapter build"}`
	sig := computeHMAC([]byte(payload), webhookKey)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-Test-Webhook", "build")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["session_id"] != "webhook-adapter" || resp["status"] != "pending_diagnosis" {
		t.Fatalf("unexpected adapter response: %+v", resp)
	}
	select {
	case intent := <-d.intentCh:
		if intent.Source != "webhook" || intent.Reason != "webhook:example.build:failure" || intent.EventID != "adapter-delivery-1" {
			t.Fatalf("unexpected adapter intent: %+v", intent)
		}
		for _, want := range []string{"example build failed", "repo=example/repo", "ref=main"} {
			if !strings.Contains(intent.Context, want) {
				t.Fatalf("adapter intent context missing %q:\n%s", want, intent.Context)
			}
		}
	default:
		t.Fatal("adapter-normalized failure did not enqueue diagnosis intent")
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Scheduler.LastWakeupEventID != "adapter-delivery-1" || loaded.Scheduler.LastWakeupKey == "" ||
		loaded.Budget.DailyWakeups != 1 || loaded.Wait.Kind != "event" {
		t.Fatalf("adapter wakeup metadata not persisted: scheduler=%+v budget=%+v wait=%+v", loaded.Scheduler, loaded.Budget, loaded.Wait)
	}
}

type testWebhookAdapter struct{}

func (testWebhookAdapter) Normalize(r *http.Request, body []byte, evt WebhookEvent) (WebhookEvent, WebhookInfo, bool) {
	if r.Header.Get("X-Test-Webhook") != "build" {
		return evt, WebhookInfo{}, false
	}
	var payload struct {
		Delivery string `json:"delivery"`
		Repo     string `json:"repo"`
		Ref      string `json:"ref"`
		State    string `json:"state"`
		Title    string `json:"title"`
	}
	_ = json.Unmarshal(body, &payload)
	info := WebhookInfo{
		Provider:   "example",
		Event:      "build",
		Delivery:   payload.Delivery,
		Repo:       payload.Repo,
		Ref:        payload.Ref,
		Status:     payload.State,
		Conclusion: payload.State,
		Title:      payload.Title,
		Failure:    payload.State == "failed",
	}
	if evt.Type == "" {
		evt.Type = "example.build"
	}
	if evt.EventID == "" {
		evt.EventID = payload.Delivery
	}
	if evt.Summary == "" {
		evt.Summary = "example build " + payload.State + " repo=" + payload.Repo + " ref=" + payload.Ref
	}
	return evt, info, true
}

func TestWebhookQueuesDiagnosisForWaitEventFailure(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "webhook-wait-conclusion.jsonl")
	if err := os.WriteFile(sess, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "webhook-wait-conclusion",
		Goal:      agent.RuntimeGoalMeta{Text: "wait for successful CI", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     "github.workflow_run",
			EventStatus:     "completed",
			EventConclusion: "success",
			Reason:          "waiting for CI success",
			Subject:         "PR #42",
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 2,
			WindowStartedAt:  budgetWindowStart(time.Now().UTC()),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	secret := "wait-conclusion-secret"
	d := New(Options{
		SessionDir: dir,
		Webhook:    &WebhookConfig{Secret: secret, Enabled: true},
	})
	d.scanSessions()

	payload := `{"action":"completed","repository":{"full_name":"esengine/DeepSeek-Reasonix"},"workflow_run":{"status":"completed","conclusion":"failure","pull_requests":[{"number":42}],"head_branch":"feature/agentos"}}`
	sig := computeHMAC([]byte(payload), secret)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-GitHub-Delivery", "delivery-failure")
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "pending_diagnosis" {
		t.Fatalf("status = %v, want pending_diagnosis", resp["status"])
	}
	select {
	case intent := <-d.intentCh:
		if intent.Source != "webhook" || intent.Reason != "webhook:github.workflow_run:failure" {
			t.Fatalf("unexpected diagnosis intent: %+v", intent)
		}
		if !strings.Contains(intent.Context, "CI finished without the awaited successful conclusion") ||
			!strings.Contains(intent.Context, "conclusion=failure") ||
			!strings.Contains(intent.Context, "keep waiting for the success condition") {
			t.Fatalf("diagnosis context missing guidance:\n%s", intent.Context)
		}
	default:
		t.Fatal("failure conclusion should enqueue diagnosis intent")
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "queued" || loaded.Wait.Kind != "event" || loaded.Wait.EventConclusion != "success" {
		t.Fatalf("diagnosis should queue run while preserving wait: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	if loaded.Budget.DailyWakeups != 1 {
		t.Fatalf("diagnosis wakeup should consume budget: %+v", loaded.Budget)
	}
	if loaded.Scheduler.LastWakeupKey == "" {
		t.Fatalf("diagnosis wakeup should persist semantic wakeup key: %+v", loaded.Scheduler)
	}

	req = httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-GitHub-Delivery", "delivery-failure-replay")
	rr = httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("replay webhook status = %d body=%s", rr.Code, rr.Body.String())
	}
	resp = map[string]any{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if resp["status"] != "duplicate" {
		t.Fatalf("replay status = %v, want duplicate", resp["status"])
	}
	select {
	case intent := <-d.intentCh:
		t.Fatalf("semantic duplicate should not enqueue intent: %+v", intent)
	default:
	}
	loaded, ok, err = agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after replay: err=%v ok=%v", err, ok)
	}
	if loaded.Budget.DailyWakeups != 1 {
		t.Fatalf("semantic duplicate should not consume budget: %+v", loaded.Budget)
	}
}

func TestFileWatcherIgnorePatterns(t *testing.T) {
	fw := NewFileWatcher(nil, nil)

	tests := []struct {
		name    string
		pattern []string
		expect  bool
	}{
		{"node_modules", nil, true},
		{".git", nil, true},
		{"main.go", nil, false},
		{"secret.key", nil, true},
		{"backup.tmp", []string{"*.tmp"}, true},
		{"data.json", []string{"*.tmp"}, false},
	}

	for _, tt := range tests {
		got := fw.shouldIgnore(tt.name, tt.pattern)
		if got != tt.expect {
			t.Errorf("shouldIgnore(%q, %v) = %v, want %v", tt.name, tt.pattern, got, tt.expect)
		}
	}
}

func TestFileWatcherRegisterUnregister(t *testing.T) {
	fw := NewFileWatcher(nil, nil)

	cfg := FileWatchConfig{
		Paths:   []string{"/tmp/test"},
		Enabled: true,
	}
	fw.Register("sess1", cfg)

	fw.mu.Lock()
	if _, ok := fw.watches["sess1"]; !ok {
		t.Error("session not registered")
	}
	fw.mu.Unlock()

	fw.Unregister("sess1")
	fw.mu.Lock()
	if _, ok := fw.watches["sess1"]; ok {
		t.Error("session should be unregistered")
	}
	fw.mu.Unlock()
}

// --- helpers ---

func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
