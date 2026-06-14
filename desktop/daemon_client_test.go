package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/daemon"
	"reasonix/internal/provider"
)

func TestDesktopDaemonClientStatusAndActionsUseAuthToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(daemon.TokenFile(dir), []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	activePath := filepath.Join(dir, "active.jsonl")
	app := &App{
		tabs:        map[string]*WorkspaceTab{"tab": {ID: "tab", Scope: "global", TopicID: "topic", Ctrl: controllerWithContent(t, activePath)}},
		activeTabID: "tab",
	}

	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Reasonix-Daemon-Token"); got != "secret-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		seen[r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(daemon.StatusResponse{Status: "running", Addr: "127.0.0.1:19840", Sessions: 1, Uptime: "1s", PID: 123})
		case "/approvals":
			_ = json.NewEncoder(w).Encode(daemon.ApprovalDeskResponse{Items: []daemon.ApprovalDeskItem{{
				SessionID:  "session-1",
				Kind:       "ask",
				ID:         "ask-1",
				Reason:     "user answer required",
				GoalText:   "ship release",
				GoalStatus: "running",
				RunStatus:  "waiting_ask",
				Active:     true,
				Questions: []daemon.ApprovalDeskQuestion{{
					ID:     "q1",
					Prompt: "Ship now?",
					Options: []daemon.ApprovalDeskOption{
						{Label: "yes"},
						{Label: "no", Description: "hold release"},
					},
				}},
			}}})
		case "/continue-goal":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["session_id"] != "session-1" || req["reason"] != "desktop" {
				t.Fatalf("continue body = %+v", req)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/stop", "/approvals/approve", "/asks/answer":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/schedule":
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["session_id"] != "session-1" || req["enabled"] != false {
				t.Fatalf("disable schedule body = %+v", req)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/watch":
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["session_id"] != "session-1" || req["enabled"] != false {
				t.Fatalf("disable watch body = %+v", req)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	status := app.DaemonStatus(server.URL)
	if !status.Connected || status.Sessions != 1 || status.PID != 123 {
		t.Fatalf("DaemonStatus = %+v", status)
	}
	approvals, err := app.ListDaemonApprovals(server.URL)
	if err != nil {
		t.Fatalf("ListDaemonApprovals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].SessionID != "session-1" || approvals[0].ID != "ask-1" ||
		len(approvals[0].Questions) != 1 || approvals[0].Questions[0].Options[1].Description != "hold release" {
		t.Fatalf("ListDaemonApprovals = %+v", approvals)
	}
	if err := app.ContinueDaemonGoal("session-1", server.URL); err != nil {
		t.Fatalf("ContinueDaemonGoal: %v", err)
	}
	if err := app.StopDaemonSession("session-1", server.URL); err != nil {
		t.Fatalf("StopDaemonSession: %v", err)
	}
	if err := app.DisableDaemonSchedule("session-1", server.URL); err != nil {
		t.Fatalf("DisableDaemonSchedule: %v", err)
	}
	if err := app.DisableDaemonWatch("session-1", server.URL); err != nil {
		t.Fatalf("DisableDaemonWatch: %v", err)
	}
	if err := app.ApproveDaemon("session-1", "approval-1", true, true, false, server.URL); err != nil {
		t.Fatalf("ApproveDaemon: %v", err)
	}
	if err := app.AnswerDaemonQuestion("session-1", "ask-1", []QuestionAnswer{{QuestionID: "q1", Selected: []string{"yes"}}}, "", server.URL); err != nil {
		t.Fatalf("AnswerDaemonQuestion: %v", err)
	}
	for _, path := range []string{"/status", "/approvals", "/continue-goal", "/stop", "/schedule", "/watch", "/approvals/approve", "/asks/answer"} {
		if !seen[path] {
			t.Fatalf("daemon endpoint %s was not called; seen=%+v", path, seen)
		}
	}
}

func TestDesktopDaemonSessionsAndOpenResumeTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	activePath := filepath.Join(dir, "active.jsonl")
	targetPath := filepath.Join(dir, "daemon-target.jsonl")
	target := agent.NewSession("")
	target.Add(provider.Message{Role: provider.RoleUser, Content: "daemon target prompt"})
	target.Add(provider.Message{Role: provider.RoleAssistant, Content: "daemon target answer"})
	if err := target.Save(targetPath); err != nil {
		t.Fatalf("save target session: %v", err)
	}
	if err := agent.SaveBranchMeta(targetPath, agent.BranchMeta{
		ID:         "daemon-target",
		Scope:      "global",
		TopicID:    "topic-daemon",
		TopicTitle: "Daemon topic",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	tab := &WorkspaceTab{ID: "tab", Scope: "global", TopicID: "topic-daemon", TopicTitle: "Daemon topic", Ctrl: controllerWithContent(t, activePath)}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}, activeTabID: "tab"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sessions":
			next := time.Date(2026, 6, 14, 9, 30, 0, 0, time.UTC)
			_ = json.NewEncoder(w).Encode(daemon.SessionsResponse{Sessions: []daemon.SessionView{{
				ID:                  "daemon-target",
				Path:                targetPath,
				GoalText:            "finish daemon session",
				GoalStatus:          "running",
				RunStatus:           "waiting_approval",
				WaitKind:            "approval",
				WaitID:              "approval-1",
				Active:              true,
				Scope:               "global",
				TopicID:             "topic-daemon",
				TopicTitle:          "Daemon topic",
				NextWakeupAt:        &next,
				DailyWakeupLimit:    3,
				DailyWakeups:        1,
				MaxGoalAutoTurns:    4,
				DailyModelCallLimit: 5,
				DailyModelCalls:     2,
				DailyModelCostLimit: 1.5,
				DailyModelCost:      0.25,
				ModelCostCurrency:   "$",
				Scheduled:           true,
				Watched:             true,
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	sessions, err := app.ListDaemonSessions(server.URL)
	if err != nil {
		t.Fatalf("ListDaemonSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "daemon-target" || sessions[0].TopicTitle != "Daemon topic" {
		t.Fatalf("unexpected daemon sessions: %+v", sessions)
	}
	if sessions[0].NextWakeupAt == nil || sessions[0].DailyWakeupLimit != 3 || sessions[0].DailyWakeups != 1 ||
		sessions[0].MaxGoalAutoTurns != 4 || sessions[0].DailyModelCallLimit != 5 || sessions[0].DailyModelCalls != 2 ||
		sessions[0].DailyModelCostLimit != 1.5 || sessions[0].DailyModelCost != 0.25 || !sessions[0].Scheduled || !sessions[0].Watched {
		t.Fatalf("daemon overview fields missing: %+v", sessions[0])
	}

	meta, err := app.OpenDaemonSession("daemon-target", server.URL)
	if err != nil {
		t.Fatalf("OpenDaemonSession: %v", err)
	}
	if !meta.Active {
		t.Fatalf("OpenDaemonSession meta = %+v, want active tab", meta)
	}
	opened := app.tabs[meta.ID]
	if opened == nil || opened.Ctrl == nil {
		t.Fatalf("opened tab not ready: meta=%+v tab=%+v", meta, opened)
	}
	if got := opened.Ctrl.SessionPath(); got != targetPath {
		t.Fatalf("controller session path = %q, want %q", got, targetPath)
	}
	history := app.HistoryForTab(meta.ID)
	var joined []string
	for _, msg := range history {
		joined = append(joined, msg.Content)
	}
	if !strings.Contains(strings.Join(joined, "\n"), "daemon target prompt") {
		t.Fatalf("history did not resume target transcript: %+v", history)
	}
}

func TestDaemonBaseURLRejectsNonLoopback(t *testing.T) {
	if _, err := daemonBaseURL("https://127.0.0.1:19840"); err == nil {
		t.Fatal("https daemon address should be rejected")
	}
	if _, err := daemonBaseURL("http://example.com:19840"); err == nil {
		t.Fatal("non-loopback daemon address should be rejected")
	}
}
