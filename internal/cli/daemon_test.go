package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/bot"
	"reasonix/internal/config"
	"reasonix/internal/daemon"
)

func TestResolveDaemonWebhookConfigDisabled(t *testing.T) {
	cfg, err := resolveDaemonWebhookConfig(false, "", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveDaemonWebhookConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("config = %+v, want nil", cfg)
	}
}

func TestResolveDaemonWebhookConfigRequiresSecret(t *testing.T) {
	_, err := resolveDaemonWebhookConfig(true, "", func(string) string { return "" })
	if err == nil {
		t.Fatal("expected missing secret error")
	}
}

func TestResolveDaemonWebhookConfigFromFlag(t *testing.T) {
	cfg, err := resolveDaemonWebhookConfig(true, "  flag-secret  ", func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveDaemonWebhookConfig: %v", err)
	}
	if cfg == nil || !cfg.Enabled || cfg.Secret != "flag-secret" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestResolveDaemonWebhookConfigFromEnv(t *testing.T) {
	cfg, err := resolveDaemonWebhookConfig(false, "", func(key string) string {
		if key != "REASONIX_DAEMON_WEBHOOK_SECRET" {
			t.Fatalf("unexpected env key %q", key)
		}
		return "env-secret"
	})
	if err != nil {
		t.Fatalf("resolveDaemonWebhookConfig: %v", err)
	}
	if cfg == nil || !cfg.Enabled || cfg.Secret != "env-secret" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestResolveDaemonLogFile(t *testing.T) {
	dir := t.TempDir()
	if got := resolveDaemonLogFile(dir, ""); got != daemon.LogFile(dir) {
		t.Fatalf("default log path = %q, want %q", got, daemon.LogFile(dir))
	}
	custom := filepath.Join(dir, "custom.log")
	if got := resolveDaemonLogFile(dir, custom); got != custom {
		t.Fatalf("custom log path = %q, want %q", got, custom)
	}
	if got := resolveDaemonLogFile(dir, "none"); got != "" {
		t.Fatalf("disabled log path = %q, want empty", got)
	}
}

func TestParseDaemonLogSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", defaultDaemonLogMaxSize},
		{"0", 0},
		{"512", 512},
		{"2KB", 2 << 10},
		{"3mb", 3 << 20},
		{"1G", 1 << 30},
	}
	for _, tc := range cases {
		got, err := parseDaemonLogSize(tc.in)
		if err != nil {
			t.Fatalf("parseDaemonLogSize(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseDaemonLogSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if _, err := parseDaemonLogSize("-1"); err == nil {
		t.Fatal("negative size should fail")
	}
	if _, err := parseDaemonLogSize("abc"); err == nil {
		t.Fatal("invalid size should fail")
	}
}

func TestNewDaemonLoggerWritesFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nested", "daemon.log")
	var stderr bytes.Buffer
	logger, closer, err := newDaemonLogger(&stderr, logPath, defaultDaemonLogMaxSize)
	if err != nil {
		t.Fatalf("newDaemonLogger: %v", err)
	}
	if closer == nil {
		t.Fatal("closer should be returned for file logging")
	}
	logger.Info("daemon test log", "component", "test")
	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	if !strings.Contains(string(b), "daemon test log") || !strings.Contains(stderr.String(), "daemon test log") {
		t.Fatalf("log should be written to file and stderr; file=%q stderr=%q", string(b), stderr.String())
	}
}

func TestNewDaemonLoggerCanDisableFile(t *testing.T) {
	logger, closer, err := newDaemonLogger(io.Discard, "", defaultDaemonLogMaxSize)
	if err != nil {
		t.Fatalf("newDaemonLogger: %v", err)
	}
	if logger == nil || closer != nil {
		t.Fatalf("logger=%v closer=%v, want logger without closer", logger, closer)
	}
}

func TestNewDaemonLoggerRotatesLargeLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("WriteFile log: %v", err)
	}
	if err := os.WriteFile(logPath+".1", []byte("old backup"), 0o600); err != nil {
		t.Fatalf("WriteFile backup: %v", err)
	}

	logger, closer, err := newDaemonLogger(io.Discard, logPath, 5)
	if err != nil {
		t.Fatalf("newDaemonLogger: %v", err)
	}
	logger.Info("fresh log")
	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	backup, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("ReadFile backup: %v", err)
	}
	if string(backup) != "0123456789" {
		t.Fatalf("backup = %q, want original log", backup)
	}
	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if !strings.Contains(string(current), "fresh log") || strings.Contains(string(current), "0123456789") {
		t.Fatalf("current log should be fresh after rotation: %q", current)
	}
}

func TestNewDaemonLoggerCanDisableRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("WriteFile log: %v", err)
	}
	logger, closer, err := newDaemonLogger(io.Discard, logPath, 0)
	if err != nil {
		t.Fatalf("newDaemonLogger: %v", err)
	}
	logger.Info("appended")
	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}
	if _, err := os.Stat(logPath + ".1"); !os.IsNotExist(err) {
		t.Fatalf("backup should not exist when rotation disabled: %v", err)
	}
	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if !strings.Contains(string(current), "0123456789") || !strings.Contains(string(current), "appended") {
		t.Fatalf("log should append without rotation: %q", current)
	}
}

func TestDaemonTokenRotateCommandWritesNewTokenWithoutPrintingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("old-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}

	out := captureStdout(t, func() {
		if rc := daemonCommand([]string{"token", "rotate", "--dir", dir}); rc != 0 {
			t.Fatalf("daemon token rotate rc = %d, want 0", rc)
		}
	})

	b, err := os.ReadFile(daemon.TokenFile(dir))
	if err != nil {
		t.Fatalf("ReadFile token: %v", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" || token == "old-token" {
		t.Fatalf("token was not rotated: %q", token)
	}
	if strings.Contains(out, token) {
		t.Fatal("rotate output should not print token")
	}
	for _, want := range []string{
		"daemon token rotated:",
		daemon.TokenFile(dir),
		"restart any running daemon",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rotate output missing %q:\n%s", want, out)
		}
	}
}

func TestDaemonSessionsCommandFiltersAndPrintsOverview(t *testing.T) {
	next := time.Date(2026, 6, 14, 9, 30, 0, 0, time.UTC)
	seenPath := ""
	seenQuery := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.SessionsResponse{Sessions: []daemon.SessionView{{
			ID:                  "abc",
			Path:                "/tmp/abc.jsonl",
			Scope:               "project",
			GoalText:            "finish daemon session overview",
			GoalStatus:          "running",
			RunStatus:           "waiting_event",
			WaitKind:            "event",
			WaitID:              "ci-green",
			NextWakeupAt:        &next,
			DailyWakeupLimit:    3,
			DailyWakeups:        1,
			DailyModelCallLimit: 5,
			DailyModelCalls:     2,
			DailyModelCostLimit: 1.5,
			DailyModelCost:      0.25,
			ModelCostCurrency:   "$",
			BudgetBlockedReason: "daily budget exhausted",
			Scheduled:           true,
			Watched:             true,
			Active:              true,
		}}})
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonSessions([]string{"--addr", addr, "--scope", "project", "--workspace-root", "/repo", "--status", "waiting"}); rc != 0 {
			t.Fatalf("daemonSessions rc = %d, want 0", rc)
		}
	})

	if seenPath != "/sessions" {
		t.Fatalf("seenPath = %q, want /sessions", seenPath)
	}
	for _, want := range []string{"scope=project", "workspace_root=%2Frepo", "status=waiting"} {
		if !strings.Contains(seenQuery, want) {
			t.Fatalf("query missing %q: %s", want, seenQuery)
		}
	}
	for _, want := range []string{
		"abc  scope=project",
		"run=waiting_event",
		"wait=event:ci-green",
		"next=2026-06-14T09:30:00Z",
		"budget=wakeups 1/3,models 2/5,cost $0.25/$1.5,blocked",
		"scheduled=true",
		"watched=true",
		"active=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("sessions output missing %q:\n%s", want, out)
		}
	}
}

func TestPrepareBotGatewayBuildsGateway(t *testing.T) {
	cfg := testBotGatewayConfig()
	cfg.Bot.Model = "bot-model"
	cfg.Bot.QQ.Enabled = true

	prepared, err := prepareBotGateway(&cfg, botGatewayOptions{Channels: "qq"}, nil, nil)
	if err != nil {
		t.Fatalf("prepareBotGateway: %v", err)
	}
	if prepared == nil || prepared.Gateway == nil {
		t.Fatalf("gateway not prepared: %+v", prepared)
	}
	if prepared.Model != "bot-model" || prepared.ChannelSummary != "qq" {
		t.Fatalf("unexpected prepared gateway: %+v", prepared)
	}
}

func TestDaemonApprovalsCommandListsPendingItems(t *testing.T) {
	seenPath := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.ApprovalDeskResponse{Items: []daemon.ApprovalDeskItem{{
			SessionID: "session-approval-123456",
			Kind:      "approval",
			ID:        "approval-1",
			Tool:      "shell",
			Subject:   "go test ./...",
			RunStatus: "waiting_approval",
			Active:    true,
		}, {
			SessionID: "session-ask-123456",
			Kind:      "ask",
			ID:        "ask-1",
			Reason:    "user answer required",
			RunStatus: "waiting_ask",
			Active:    true,
			Questions: []daemon.ApprovalDeskQuestion{{
				ID:     "q1",
				Prompt: "Ship now?",
				Options: []daemon.ApprovalDeskOption{
					{Label: "yes"},
					{Label: "no"},
				},
			}},
		}}})
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonApprovals([]string{"--addr", addr}); rc != 0 {
			t.Fatalf("daemonApprovals rc = %d, want 0", rc)
		}
	})

	if seenPath != "/approvals" {
		t.Fatalf("seenPath = %q, want /approvals", seenPath)
	}
	for _, want := range []string{
		"session-appr",
		"approval:approval-1",
		"tool=shell",
		"reasonix daemon approve --session session-approval-123456 --approval approval-1",
		"ask:ask-1",
		"q1: Ship now? [yes / no]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("approvals output missing %q:\n%s", want, out)
		}
	}
}

func TestDaemonApprovalsCommandJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.ApprovalDeskResponse{Items: []daemon.ApprovalDeskItem{{
			SessionID: "session-1",
			Kind:      "approval",
			ID:        "approval-1",
		}}})
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonApprovals([]string{"--addr", addr, "--json"}); rc != 0 {
			t.Fatalf("daemonApprovals --json rc = %d, want 0", rc)
		}
	})

	var resp daemon.ApprovalDeskResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json output did not decode: %v\n%s", err, out)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != "approval-1" {
		t.Fatalf("decoded approvals = %+v", resp)
	}
}

func TestDaemonScheduleCommandSendsScopedPayload(t *testing.T) {
	seenPath := ""
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonScheduleCmd([]string{
			"--addr", addr,
			"--scope", "project",
			"--workspace-root", "/repo",
			"--daily-at", "07:00",
			"--timezone", "Asia/Shanghai",
		}); rc != 0 {
			t.Fatalf("daemonScheduleCmd rc = %d, want 0", rc)
		}
	})

	if seenPath != "/schedule" {
		t.Fatalf("seenPath = %q, want /schedule", seenPath)
	}
	if payload["scope"] != "project" || payload["workspace_root"] != "/repo" || payload["daily_at"] != "07:00" || payload["timezone"] != "Asia/Shanghai" {
		t.Fatalf("unexpected schedule payload: %+v", payload)
	}
	if enabled, ok := payload["enabled"].(bool); !ok || !enabled {
		t.Fatalf("enabled = %v, want true", payload["enabled"])
	}
	if _, ok := payload["session_id"]; ok {
		t.Fatalf("schedule payload should not include session_id: %+v", payload)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("schedule output = %q, want ok response", out)
	}
}

func TestDaemonDisableScheduleCommandSendsDisabledPayload(t *testing.T) {
	seenPath := ""
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"enabled":false}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonDisableScheduleCmd([]string{
			"--addr", addr,
			"--scope", "project",
			"--workspace-root", "/repo",
		}); rc != 0 {
			t.Fatalf("daemonDisableScheduleCmd rc = %d, want 0", rc)
		}
	})

	if seenPath != "/schedule" {
		t.Fatalf("seenPath = %q, want /schedule", seenPath)
	}
	if payload["scope"] != "project" || payload["workspace_root"] != "/repo" {
		t.Fatalf("unexpected disable schedule payload: %+v", payload)
	}
	if enabled, ok := payload["enabled"].(bool); !ok || enabled {
		t.Fatalf("enabled = %v, want false", payload["enabled"])
	}
	if !strings.Contains(out, `"enabled":false`) {
		t.Fatalf("disable schedule output = %q, want disabled response", out)
	}
}

func TestDaemonDisableWatchCommandSendsDisabledPayload(t *testing.T) {
	seenPath := ""
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"enabled":false}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonDisableWatchCmd([]string{"--addr", addr, "--session", "session-watch"}); rc != 0 {
			t.Fatalf("daemonDisableWatchCmd rc = %d, want 0", rc)
		}
	})

	if seenPath != "/watch" {
		t.Fatalf("seenPath = %q, want /watch", seenPath)
	}
	if payload["session_id"] != "session-watch" {
		t.Fatalf("unexpected disable watch payload: %+v", payload)
	}
	if enabled, ok := payload["enabled"].(bool); !ok || enabled {
		t.Fatalf("enabled = %v, want false", payload["enabled"])
	}
	if _, ok := payload["paths"]; ok {
		t.Fatalf("disable watch should not clear paths: %+v", payload)
	}
	if !strings.Contains(out, `"enabled":false`) {
		t.Fatalf("disable watch output = %q, want disabled response", out)
	}
}

func TestDaemonCIWatchCommandSendsWorkflowPayload(t *testing.T) {
	seenPath := ""
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonCIWatchCmd([]string{
			"--addr", addr,
			"--session", "session-ci",
			"--repo", "esengine/DeepSeek-Reasonix",
			"--pr", "42",
		}); rc != 0 {
			t.Fatalf("daemonCIWatchCmd rc = %d, want 0", rc)
		}
	})

	if seenPath != "/wait-event" {
		t.Fatalf("seenPath = %q, want /wait-event", seenPath)
	}
	want := map[string]interface{}{
		"session_id":       "session-ci",
		"event_source":     "github.workflow_run",
		"event_status":     "completed",
		"event_conclusion": "success",
		"reason":           "waiting for CI success",
		"subject":          "esengine/DeepSeek-Reasonix PR #42",
	}
	for key, value := range want {
		if payload[key] != value {
			t.Fatalf("payload[%s] = %v, want %v; payload=%+v", key, payload[key], value, payload)
		}
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("ci-watch output = %q, want ok response", out)
	}
}

func TestDaemonCIWatchCommandSendsStatusPayload(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	_ = captureStdout(t, func() {
		if rc := daemonCIWatchCmd([]string{
			"--addr", addr,
			"--session", "session-status",
			"--source", "status",
			"--subject", "branch main",
		}); rc != 0 {
			t.Fatalf("daemonCIWatchCmd status rc = %d, want 0", rc)
		}
	})

	if payload["event_source"] != "github.status" || payload["event_conclusion"] != "success" || payload["subject"] != "branch main" {
		t.Fatalf("unexpected status payload: %+v", payload)
	}
	if _, ok := payload["event_status"]; ok {
		t.Fatalf("status payload should not include event_status: %+v", payload)
	}
}

func TestDaemonCIWatchCommandRejectsUnknownSource(t *testing.T) {
	if rc := daemonCIWatchCmd([]string{"--session", "session-ci", "--source", "push"}); rc != 2 {
		t.Fatalf("daemonCIWatchCmd unknown source rc = %d, want 2", rc)
	}
}

func TestDaemonDailyTriageCommandConfiguresScheduleAndBudget(t *testing.T) {
	var paths []string
	var payloads []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonDailyTriageCmd([]string{
			"--addr", addr,
			"--session", "triage-session",
			"--daily-at", "08:45",
			"--timezone", "Asia/Shanghai",
			"--daily-wakeups", "2",
		}); rc != 0 {
			t.Fatalf("daemonDailyTriageCmd rc = %d, want 0", rc)
		}
	})

	if got, want := strings.Join(paths, ","), "/schedule,/budget"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	if len(payloads) != 2 {
		t.Fatalf("payload count = %d, want 2", len(payloads))
	}
	schedule := payloads[0]
	if schedule["session_id"] != "triage-session" || schedule["daily_at"] != "08:45" || schedule["timezone"] != "Asia/Shanghai" || schedule["enabled"] != true {
		t.Fatalf("unexpected schedule payload: %+v", schedule)
	}
	budget := payloads[1]
	if budget["session_id"] != "triage-session" || budget["daily_wakeup_limit"] != float64(2) || budget["max_goal_auto_turns"] != float64(0) {
		t.Fatalf("unexpected budget payload: %+v", budget)
	}
	if strings.Count(out, `"ok":true`) != 2 {
		t.Fatalf("daily-triage output = %q, want two ok responses", out)
	}
}

func TestDaemonBudgetCommandConfiguresProjectAggregateQuota(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/budget" {
			t.Fatalf("path = %q, want /budget", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	_ = captureStdout(t, func() {
		if rc := daemonBudgetCmd([]string{
			"--addr", addr,
			"--scope", "project",
			"--workspace-root", "/repo",
			"--daily-model-calls", "3",
			"--daily-model-cost", "1.5",
		}); rc != 0 {
			t.Fatalf("daemonBudgetCmd project scope rc = %d, want 0", rc)
		}
	})

	if payload["scope"] != "project" || payload["workspace_root"] != "/repo" ||
		payload["daily_model_call_limit"] != float64(3) || payload["daily_model_cost_limit"] != 1.5 {
		t.Fatalf("unexpected project budget payload: %+v", payload)
	}
	if _, ok := payload["session_id"]; ok {
		t.Fatalf("scope budget should not include session_id: %+v", payload)
	}
}

func TestDaemonBudgetsCommandPrintsAggregateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/budgets" {
			t.Fatalf("path = %q, want /budgets", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"budgets":[{"scope":"global","session_count":2,"daily_model_call_limit":3,"daily_model_calls":1}]}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonBudgetsCmd([]string{"--addr", addr}); rc != 0 {
			t.Fatalf("daemonBudgetsCmd rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `"scope": "global"`) || !strings.Contains(out, `"daily_model_calls": 1`) {
		t.Fatalf("budgets output = %q", out)
	}
}

func TestDaemonTemplatesCommandListsCopyableTemplates(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := daemonTemplatesCmd(nil); rc != 0 {
			t.Fatalf("daemonTemplatesCmd rc = %d, want 0", rc)
		}
	})
	for _, id := range []string{"daily-triage", "ci-watcher", "release-assist", "repo-health"} {
		if !strings.Contains(out, id) {
			t.Fatalf("templates output missing %q: %s", id, out)
		}
	}
	if !strings.Contains(out, "goal starter:") {
		t.Fatalf("templates output should include goal starters: %s", out)
	}
}

func TestDaemonApplyTemplateConfiguresEachScenario(t *testing.T) {
	cases := []struct {
		name      string
		template  string
		extraArgs []string
		wantPaths string
		wantLabel string
	}{
		{name: "daily triage", template: "daily-triage", wantPaths: "/schedule,/budget", wantLabel: "daily-triage"},
		{name: "ci watcher alias", template: "ci-watch", extraArgs: []string{"--repo", "owner/repo", "--pr", "42"}, wantPaths: "/wait-event", wantLabel: "ci-watcher"},
		{name: "release assist", template: "release-assist", extraArgs: []string{"--paths", "CHANGELOG.md,go.mod"}, wantPaths: "/wait-file", wantLabel: "release-assist"},
		{name: "repo health", template: "repo-health", wantPaths: "/schedule,/budget", wantLabel: "repo-health"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			var payloads []map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				var payload map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				payloads = append(payloads, payload)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			addr := strings.TrimPrefix(server.URL, "http://")

			args := []string{"--addr", addr, "--template", tc.template, "--session", "template-session"}
			args = append(args, tc.extraArgs...)
			out := captureStdout(t, func() {
				if rc := daemonApplyTemplateCmd(args); rc != 0 {
					t.Fatalf("daemonApplyTemplateCmd rc = %d, want 0", rc)
				}
			})

			if got := strings.Join(paths, ","); got != tc.wantPaths {
				t.Fatalf("paths = %q, want %q", got, tc.wantPaths)
			}
			if len(payloads) == 0 || payloads[0]["session_id"] != "template-session" {
				t.Fatalf("template should configure the target session: %+v", payloads)
			}
			if !strings.Contains(out, "template: "+tc.wantLabel) || !strings.Contains(out, "goal starter:") {
				t.Fatalf("template output missing label/starter: %q", out)
			}
		})
	}
}

func TestDaemonDailyTriageCommandCanSkipBudget(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	_ = captureStdout(t, func() {
		if rc := daemonDailyTriageCmd([]string{
			"--addr", addr,
			"--session", "triage-session",
			"--daily-wakeups", "-1",
		}); rc != 0 {
			t.Fatalf("daemonDailyTriageCmd skip budget rc = %d, want 0", rc)
		}
	})

	if got, want := strings.Join(paths, ","), "/schedule"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestDaemonReleaseAssistCommandSendsWaitFilePayload(t *testing.T) {
	seenPath := ""
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonReleaseAssistCmd([]string{
			"--addr", addr,
			"--session", "release-session",
			"--paths", "CHANGELOG.md,cmd/version.go",
			"--ignore", "*.tmp,dist",
			"--debounce", "5s",
			"--subject", "v1.2.3",
		}); rc != 0 {
			t.Fatalf("daemonReleaseAssistCmd rc = %d, want 0", rc)
		}
	})

	if seenPath != "/wait-file" {
		t.Fatalf("seenPath = %q, want /wait-file", seenPath)
	}
	if payload["session_id"] != "release-session" || payload["reason"] != "release files changed; check changelog and version before publishing" ||
		payload["subject"] != "v1.2.3" || payload["debounce"] != "5s" {
		t.Fatalf("unexpected release-assist payload: %+v", payload)
	}
	paths, ok := payload["paths"].([]interface{})
	if !ok || len(paths) != 2 || paths[0] != "CHANGELOG.md" || paths[1] != "cmd/version.go" {
		t.Fatalf("unexpected paths payload: %+v", payload["paths"])
	}
	ignore, ok := payload["ignore_patterns"].([]interface{})
	if !ok || len(ignore) != 2 || ignore[0] != "*.tmp" || ignore[1] != "dist" {
		t.Fatalf("unexpected ignore payload: %+v", payload["ignore_patterns"])
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("release-assist output = %q, want ok response", out)
	}
}

func TestDaemonReleaseAssistCommandRequiresPaths(t *testing.T) {
	if rc := daemonReleaseAssistCmd([]string{"--session", "release-session", "--paths", " , "}); rc != 2 {
		t.Fatalf("daemonReleaseAssistCmd empty paths rc = %d, want 2", rc)
	}
}

func TestDaemonRepoHealthCommandConfiguresScheduleAndBudget(t *testing.T) {
	var paths []string
	var payloads []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	out := captureStdout(t, func() {
		if rc := daemonRepoHealthCmd([]string{
			"--addr", addr,
			"--session", "health-session",
			"--daily-at", "10:30",
			"--timezone", "Asia/Shanghai",
			"--daily-wakeups", "1",
			"--max-goal-auto-turns", "2",
			"--daily-model-calls", "4",
			"--daily-model-cost", "0.25",
		}); rc != 0 {
			t.Fatalf("daemonRepoHealthCmd rc = %d, want 0", rc)
		}
	})

	if got, want := strings.Join(paths, ","), "/schedule,/budget"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	if len(payloads) != 2 {
		t.Fatalf("payload count = %d, want 2", len(payloads))
	}
	schedule := payloads[0]
	if schedule["session_id"] != "health-session" || schedule["daily_at"] != "10:30" || schedule["timezone"] != "Asia/Shanghai" || schedule["enabled"] != true {
		t.Fatalf("unexpected schedule payload: %+v", schedule)
	}
	budget := payloads[1]
	if budget["session_id"] != "health-session" ||
		budget["daily_wakeup_limit"] != float64(1) ||
		budget["max_goal_auto_turns"] != float64(2) ||
		budget["daily_model_call_limit"] != float64(4) ||
		budget["daily_model_cost_limit"] != 0.25 {
		t.Fatalf("unexpected budget payload: %+v", budget)
	}
	if strings.Count(out, `"ok":true`) != 2 {
		t.Fatalf("repo-health output = %q, want two ok responses", out)
	}
}

func TestDaemonRepoHealthCommandCanSkipBudget(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")

	_ = captureStdout(t, func() {
		if rc := daemonRepoHealthCmd([]string{
			"--addr", addr,
			"--session", "health-session",
			"--daily-wakeups", "-1",
			"--max-goal-auto-turns", "-1",
		}); rc != 0 {
			t.Fatalf("daemonRepoHealthCmd skip budget rc = %d, want 0", rc)
		}
	})

	if got, want := strings.Join(paths, ","), "/schedule"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestDaemonRepoHealthCommandRejectsNegativeBudget(t *testing.T) {
	if rc := daemonRepoHealthCmd([]string{"--session", "health-session", "--daily-model-calls", "-2"}); rc != 2 {
		t.Fatalf("daemonRepoHealthCmd negative model calls rc = %d, want 2", rc)
	}
}

func TestPrepareBotGatewayRejectsUnsafeConfig(t *testing.T) {
	cfg := testBotGatewayConfig()
	cfg.Bot.Enabled = false
	if _, err := prepareBotGateway(&cfg, botGatewayOptions{}, nil, nil); err == nil {
		t.Fatal("disabled bot config should be rejected")
	}

	cfg = testBotGatewayConfig()
	cfg.Bot.Allowlist.AllowAll = false
	cfg.Bot.Allowlist.Enabled = false
	if _, err := prepareBotGateway(&cfg, botGatewayOptions{}, nil, nil); err == nil {
		t.Fatal("bot config without allowlist should be rejected")
	}

	cfg = testBotGatewayConfig()
	if _, err := prepareBotGateway(&cfg, botGatewayOptions{Channels: "qq"}, nil, nil); err == nil {
		t.Fatal("requested disabled channel should be rejected")
	}
}

func TestResolveBotEnabledPlatformsWarnsUnknown(t *testing.T) {
	cfg := testBotGatewayConfig().Bot
	cfg.QQ.Enabled = true
	var warnings []string
	enabled := resolveBotEnabledPlatforms(cfg, "qq,nope", func(format string, args ...interface{}) {
		warnings = append(warnings, format)
	})
	if !enabled[bot.PlatformQQ] || len(warnings) != 1 {
		t.Fatalf("enabled=%+v warnings=%+v", enabled, warnings)
	}
}

func testBotGatewayConfig() config.Config {
	return config.Config{
		DefaultModel: "default-model",
		Bot: config.BotConfig{
			Enabled: true,
			Allowlist: config.BotAllowlist{
				AllowAll: true,
			},
		},
	}
}

func TestBuildDaemonDoctorReportSummarizesRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}
	sessionPath := filepath.Join(dir, "agentos.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile session: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		SessionID: "agentos",
		Goal:      agent.RuntimeGoalMeta{Text: "ship daemon", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "interrupted"},
		Wait:      agent.RuntimeWaitMeta{Kind: "event", EventSource: "github"},
		Scheduler: agent.RuntimeSchedMeta{Enabled: true, Interval: time.Hour},
		FileWatch: agent.RuntimeWatchMeta{Enabled: true, Paths: []string{"src"}},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit:    1,
			DailyModelCallLimit: 2,
			DailyModelCostLimit: 0.5,
			LastBlockedReason:   "daily automatic wakeup budget exhausted for cron (1/1)",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	logPath := daemon.LogFile(dir)
	if err := os.WriteFile(logPath, []byte("daemon log\n"), 0o600); err != nil {
		t.Fatalf("WriteFile log: %v", err)
	}

	report, failed := buildDaemonDoctorReport("127.0.0.1:1", dir, "", func(string, string, string) (*http.Response, error) {
		return nil, errTestDaemonOffline{}
	})
	if failed {
		t.Fatalf("doctor should warn but not fail when daemon is offline: %+v", report.Checks)
	}
	if report.Runtime.Total != 1 || report.Runtime.ActiveGoals != 1 || report.Runtime.Interrupted != 1 ||
		report.Runtime.Waiting != 1 || report.Runtime.Scheduled != 1 || report.Runtime.Watched != 1 ||
		report.Runtime.Budgeted != 1 || report.Runtime.BudgetBlocked != 1 {
		t.Fatalf("unexpected runtime summary: %+v", report.Runtime)
	}
	if !hasDoctorCheck(report, "online", "warn") {
		t.Fatalf("missing online warning: %+v", report.Checks)
	}
	if report.LogFile != logPath || !hasDoctorCheck(report, "log", "ok") {
		t.Fatalf("missing log check: log=%q checks=%+v", report.LogFile, report.Checks)
	}
}

func TestBuildDaemonDoctorReportFailsCorruptRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.jsonl.runtime.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile runtime: %v", err)
	}

	report, failed := buildDaemonDoctorReport("127.0.0.1:1", dir, "", nil)
	if !failed {
		t.Fatalf("doctor should fail for corrupt runtime: %+v", report.Checks)
	}
	if report.Runtime.Corrupt != 1 {
		t.Fatalf("Corrupt = %d, want 1", report.Runtime.Corrupt)
	}
	if !hasDoctorCheck(report, "runtime", "fail") {
		t.Fatalf("missing runtime failure: %+v", report.Checks)
	}
}

func TestBuildDaemonDoctorReportWarnsMissingLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}

	report, failed := buildDaemonDoctorReport("127.0.0.1:1", dir, "", nil)
	if failed {
		t.Fatalf("missing log file should warn, not fail: %+v", report.Checks)
	}
	if report.LogFile != daemon.LogFile(dir) || !hasDoctorCheck(report, "log", "warn") {
		t.Fatalf("missing log warning not recorded: log=%q checks=%+v", report.LogFile, report.Checks)
	}
}

func TestBuildDaemonDoctorReportWarnsBroadTokenPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode bits are not stable on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("token\n"), 0o644); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}

	report, failed := buildDaemonDoctorReport("127.0.0.1:1", dir, "none", nil)
	if failed {
		t.Fatalf("broad token permissions should warn, not fail: %+v", report.Checks)
	}
	if !hasDoctorCheck(report, "token", "warn") {
		t.Fatalf("missing token permission warning: %+v", report.Checks)
	}
}

func TestBuildDaemonDoctorReportFailsUnwritableLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode bits are not stable on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}
	logPath := daemon.LogFile(dir)
	if err := os.WriteFile(logPath, []byte("daemon log\n"), 0o400); err != nil {
		t.Fatalf("WriteFile log: %v", err)
	}

	report, failed := buildDaemonDoctorReport("127.0.0.1:1", dir, "", nil)
	if !failed {
		t.Fatalf("unwritable log should fail: %+v", report.Checks)
	}
	if !hasDoctorCheck(report, "log", "fail") {
		t.Fatalf("missing log failure: %+v", report.Checks)
	}
}

func TestBuildDaemonDoctorReportWarnsOccupiedPort(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile token: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	report, failed := buildDaemonDoctorReport(ln.Addr().String(), dir, "none", func(string, string, string) (*http.Response, error) {
		return nil, errTestDaemonOffline{}
	})
	if failed {
		t.Fatalf("occupied port should warn, not fail: %+v", report.Checks)
	}
	if !hasDoctorCheck(report, "port", "warn") {
		t.Fatalf("missing port warning: %+v", report.Checks)
	}
}

type errTestDaemonOffline struct{}

func (errTestDaemonOffline) Error() string { return "offline" }

func hasDoctorCheck(report daemonDoctorReport, name, status string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
