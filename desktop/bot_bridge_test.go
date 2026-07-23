package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"reasonix/internal/bot"
	"reasonix/internal/event"
)

type bridgeNotifyCall struct {
	connectionID string
	domain       string
	msg          bot.OutboundMessage
}

type bridgeTestEnv struct {
	hub        *botBridgeHub
	notified   chan bridgeNotifyCall
	approves   chan [2]string // [tabID, id+":"+allow]
	answers    chan [2]string // [tabID, id]
	driven     chan [2]string // [tabID, text]
	announced  chan [2]string // [tabID, text]
	persisted  chan []bot.DesktopWatchRoute
	driveErr   error
	persistErr error
}

func tabsToSessions(tabs []TabMeta) []bot.DesktopSessionInfo {
	out := make([]bot.DesktopSessionInfo, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, bot.DesktopSessionInfo{
			TabID:         t.ID,
			Label:         t.Label,
			Workspace:     t.WorkspaceName,
			Topic:         t.TopicTitle,
			Ready:         t.Ready,
			Running:       t.Running,
			PendingPrompt: t.PendingPrompt,
		})
	}
	return out
}

func newBridgeTestEnv(tabs []TabMeta) *bridgeTestEnv {
	return newBridgeTestEnvSessions(tabsToSessions(tabs))
}

func newBridgeTestEnvSessions(sessions []bot.DesktopSessionInfo) *bridgeTestEnv {
	env := &bridgeTestEnv{
		notified:  make(chan bridgeNotifyCall, 16),
		approves:  make(chan [2]string, 16),
		answers:   make(chan [2]string, 16),
		driven:    make(chan [2]string, 16),
		announced: make(chan [2]string, 16),
		persisted: make(chan []bot.DesktopWatchRoute, 16),
	}
	env.hub = newBotBridgeHub(botBridgeDeps{
		sessions: func() []bot.DesktopSessionInfo { return sessions },
		approveTab: func(tabID, id string, allow, session, persist bool) {
			env.approves <- [2]string{tabID, fmt.Sprintf("%s:%t", id, allow)}
		},
		answerTab: func(tabID, id string, answers []QuestionAnswer) {
			env.answers <- [2]string{tabID, id}
		},
		notify: func(ctx context.Context, connectionID, domain string, msg bot.OutboundMessage) (bot.SendResult, error) {
			env.notified <- bridgeNotifyCall{connectionID: connectionID, domain: domain, msg: msg}
			return bot.SendResult{MessageID: "sent-1"}, nil
		},
		drive: func(tabID, text string, route bot.DesktopWatchRoute) error {
			if env.driveErr != nil {
				return env.driveErr
			}
			env.driven <- [2]string{tabID, text}
			return nil
		},
		announce: func(tabID, text string) {
			env.announced <- [2]string{tabID, text}
		},
		persistWatchers: func(routes []bot.DesktopWatchRoute) error {
			env.persisted <- routes
			return env.persistErr
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return env
}

func (env *bridgeTestEnv) waitNotification(t *testing.T) bridgeNotifyCall {
	t.Helper()
	select {
	case call := <-env.notified:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bridge notification")
		return bridgeNotifyCall{}
	}
}

func (env *bridgeTestEnv) expectNoNotification(t *testing.T) {
	t.Helper()
	select {
	case call := <-env.notified:
		t.Fatalf("unexpected notification: %+v", call)
	case <-time.After(100 * time.Millisecond):
	}
}

func testWatchRoute() bot.DesktopWatchRoute {
	return bot.DesktopWatchRoute{
		ConnectionID: "feishu-main",
		Domain:       "feishu",
		Platform:     bot.PlatformFeishu,
		ChatType:     bot.ChatDM,
		ChatID:       "chat-god",
	}
}

func testGroupRoute() bot.DesktopWatchRoute {
	r := testWatchRoute()
	r.ChatType = bot.ChatGroup
	r.ChatID = "group-god"
	return r
}

func TestBridgeTakeoverRejectsGroupChat(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{{TabID: "tab-1", Label: "会话一", Ready: true}})
	if _, err := env.hub.Takeover(testGroupRoute(), "tab-1"); err == nil {
		t.Fatal("takeover from a group chat must be rejected (non-admin members could otherwise drive it)")
	}
	if _, err := env.hub.Takeover(testWatchRoute(), "tab-1"); err != nil {
		t.Fatalf("DM takeover should work: %v", err)
	}
}

func TestBridgeTakeoverSwitchAnnouncesReleaseToOldTab(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{
		{TabID: "tab-a", Label: "A", Ready: true},
		{TabID: "tab-b", Label: "B", Ready: true},
	})
	route := testWatchRoute()
	if _, err := env.hub.Takeover(route, "tab-a"); err != nil {
		t.Fatalf("takeover A: %v", err)
	}
	if got := <-env.announced; got[0] != "tab-a" {
		t.Fatalf("first announce = %v, want tab-a takeover", got)
	}
	if _, err := env.hub.Takeover(route, "tab-b"); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case got := <-env.announced:
			seen[got[0]] = true
		case <-time.After(time.Second):
			t.Fatalf("missing announce after switch; seen=%v", seen)
		}
	}
	if !seen["tab-a"] || !seen["tab-b"] {
		t.Fatalf("switch should announce release to tab-a and takeover to tab-b; seen=%v", seen)
	}
}

func TestBridgeDriveInputBusyReturnsBusyMessage(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{{TabID: "tab-1", Label: "会话一", Ready: true}})
	route := testWatchRoute()
	if _, err := env.hub.Takeover(route, "tab-1"); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	<-env.announced
	env.driveErr = errDriveBusy
	_, err := env.hub.DriveInput(route, "hi")
	if err == nil || !strings.Contains(err.Error(), "正在执行中") {
		t.Fatalf("busy drive should surface a clean busy message, got %v", err)
	}
}

func TestBridgeApprovalRedactsSubjectInGroup(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}})
	env.hub.SetWatch(testGroupRoute(), true)
	<-env.persisted
	env.hub.observe("tab-1", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "a1", Tool: "bash", Subject: "rm -rf /secret"}})
	call := env.waitNotification(t)
	if strings.Contains(call.msg.Text, "rm -rf /secret") {
		t.Fatalf("group notification leaked the command line: %q", call.msg.Text)
	}
	if call.msg.Card != nil {
		for _, el := range call.msg.Card.Elements {
			if strings.Contains(el.Content, "rm -rf /secret") {
				t.Fatal("group card leaked the command line")
			}
		}
	}
}

func TestBridgeApprovalShowsSubjectInDM(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}})
	env.hub.SetWatch(testWatchRoute(), true)
	<-env.persisted
	env.hub.observe("tab-1", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "a1", Tool: "bash", Subject: "rm -rf build"}})
	call := env.waitNotification(t)
	if !strings.Contains(call.msg.Text, "rm -rf build") {
		t.Fatalf("DM notification should show the command line: %q", call.msg.Text)
	}
}

func TestBridgeAskRedactsPromptAndOptionsInGroup(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}})
	if err := env.hub.SetWatch(testGroupRoute(), true); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	<-env.persisted
	env.hub.observe("tab-1", event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID: "redacted-ask",
		Questions: []event.AskQuestion{{
			ID: "q1", Prompt: "INTERNAL_ONLY_PROMPT", Options: []event.AskOption{{Label: "CHOICE_INTERNAL"}},
		}},
	}})
	call := env.waitNotification(t)
	if strings.Contains(call.msg.Text, "INTERNAL_ONLY_PROMPT") || strings.Contains(call.msg.Text, "CHOICE_INTERNAL") {
		t.Fatalf("group notification leaked ask details: %q", call.msg.Text)
	}
	if call.msg.Card != nil {
		t.Fatal("group ask notification must not include option buttons")
	}
}

func TestBridgeSessionsIncludePendingIDs(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}})
	env.hub.observe("tab-1", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "a1", Tool: "bash"}})
	env.hub.observe("tab-1", event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "q1", Questions: []event.AskQuestion{{ID: "x", Prompt: "?"}}}})
	found := map[string]bool{}
	for _, s := range env.hub.Sessions() {
		if s.TabID != "tab-1" {
			continue
		}
		for _, p := range s.Pending {
			found[p.ID] = true
		}
	}
	if !found["a1"] || !found["q1"] {
		t.Fatalf("Sessions() should surface pending approval and ask ids; got %v", found)
	}
}

func TestBridgePersistDropsStaleSnapshot(t *testing.T) {
	env := newBridgeTestEnvSessions(nil)
	// Two subscribes: the second (newer seq) must be the persisted result even
	// though we invoke the seed-restore afterward.
	env.hub.SetWatch(testWatchRoute(), true)
	<-env.persisted
	env.hub.SetWatch(testGroupRoute(), true)
	routes := <-env.persisted
	if len(routes) != 2 {
		t.Fatalf("persisted routes = %d, want both subscriptions", len(routes))
	}
}

func TestBridgeApprovalNotifiesWatchersAndRoutesApproval(t *testing.T) {
	env := newBridgeTestEnv([]TabMeta{{ID: "tab-1", Label: "修复登录"}})
	env.hub.SetWatch(testWatchRoute(), true)

	env.hub.observe("tab-1", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: "appr-1", Tool: "bash", Subject: "rm -rf build",
	}})

	call := env.waitNotification(t)
	if call.connectionID != "feishu-main" || call.msg.ChatID != "chat-god" {
		t.Fatalf("notification routed to %s/%s, want feishu-main/chat-god", call.connectionID, call.msg.ChatID)
	}
	for _, want := range []string{"修复登录", "bash", "rm -rf build", "/desktop approve appr-1"} {
		if !strings.Contains(call.msg.Text, want) {
			t.Fatalf("notification text = %q, want it to contain %q", call.msg.Text, want)
		}
	}
	if call.msg.Card == nil {
		t.Fatal("approval notification should carry an interactive card")
	}

	feedback, err := env.hub.Approve("appr-1", true)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !strings.Contains(feedback, "先到者为准") {
		t.Fatalf("feedback = %q, want first-wins note", feedback)
	}
	select {
	case got := <-env.approves:
		if got[0] != "tab-1" || got[1] != "appr-1:true" {
			t.Fatalf("approve routed as %v, want tab-1/appr-1:true", got)
		}
	case <-time.After(time.Second):
		t.Fatal("approve was not routed to the tab")
	}

	// 同一 ID 第二次应答:pending 已清,返回未找到。
	if _, err := env.hub.Approve("appr-1", false); err == nil {
		t.Fatal("second Approve on the same id should fail")
	}
}

func TestBridgePendingRecordedWithoutWatchers(t *testing.T) {
	env := newBridgeTestEnv([]TabMeta{{ID: "tab-1", Label: "会话"}})

	env.hub.observe("tab-1", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "appr-2", Tool: "bash"}})
	env.expectNoNotification(t)

	if _, err := env.hub.Approve("appr-2", false); err != nil {
		t.Fatalf("Approve without watchers should still work: %v", err)
	}
	select {
	case got := <-env.approves:
		if got[1] != "appr-2:false" {
			t.Fatalf("deny routed as %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("deny was not routed")
	}
}

func TestBridgeTurnDoneClearsPendingAndNotifies(t *testing.T) {
	env := newBridgeTestEnv([]TabMeta{{ID: "tab-1", Label: "会话一"}})
	env.hub.SetWatch(testWatchRoute(), true)

	env.hub.observe("tab-1", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "appr-3", Tool: "bash"}})
	env.waitNotification(t)

	env.hub.observe("tab-1", event.Event{Kind: event.TurnDone})
	call := env.waitNotification(t)
	if !strings.Contains(call.msg.Text, "✅") || !strings.Contains(call.msg.Text, "会话一") {
		t.Fatalf("turn-done text = %q", call.msg.Text)
	}

	if _, err := env.hub.Approve("appr-3", true); err == nil {
		t.Fatal("pending approval should be cleared by TurnDone")
	}
}

func TestBridgeSuppressesCanceledTurnAndErrorsNotify(t *testing.T) {
	env := newBridgeTestEnv([]TabMeta{{ID: "tab-1", Label: "会话一"}})
	env.hub.SetWatch(testWatchRoute(), true)

	env.hub.observe("tab-1", event.Event{Kind: event.TurnDone, Err: errors.New("context canceled")})
	env.expectNoNotification(t)

	env.hub.observe("tab-1", event.Event{Kind: event.TurnDone, Err: errors.New("boom")})
	call := env.waitNotification(t)
	if !strings.Contains(call.msg.Text, "❌") || !strings.Contains(call.msg.Text, "boom") {
		t.Fatalf("error text = %q", call.msg.Text)
	}
}

func TestBridgeAskAnswerRoundTrip(t *testing.T) {
	env := newBridgeTestEnv([]TabMeta{{ID: "tab-2", Label: "问答会话"}})
	env.hub.SetWatch(testWatchRoute(), true)

	env.hub.observe("tab-2", event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:      "q1",
			Prompt:  "选一个方案",
			Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
		}},
	}})
	call := env.waitNotification(t)
	if !strings.Contains(call.msg.Text, "/desktop answer ask-1") {
		t.Fatalf("ask notification = %q, want answer hint", call.msg.Text)
	}
	if call.msg.Card == nil {
		t.Fatal("single-choice ask should carry option buttons")
	}

	questions, ok := env.hub.AskQuestions("ask-1")
	if !ok || len(questions) != 1 {
		t.Fatalf("AskQuestions = %v/%v", questions, ok)
	}
	if _, err := env.hub.Answer("ask-1", []event.AskAnswer{{QuestionID: "q1", Selected: []string{"B"}}}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	select {
	case got := <-env.answers:
		if got[0] != "tab-2" || got[1] != "ask-1" {
			t.Fatalf("answer routed as %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("answer was not routed")
	}
}

func TestBridgeWatchLifecycleStopsNotifications(t *testing.T) {
	env := newBridgeTestEnv(nil)
	route := testWatchRoute()

	env.hub.SetWatch(route, true)
	if !env.hub.Watching(route) {
		t.Fatal("route should be watching after SetWatch(true)")
	}
	env.hub.SetWatch(route, false)
	if env.hub.Watching(route) {
		t.Fatal("route should not be watching after SetWatch(false)")
	}

	env.hub.observe("tab-x", event.Event{Kind: event.TurnDone})
	env.expectNoNotification(t)
}

func TestBridgeSetWatchPersistsAndSeedRestores(t *testing.T) {
	env := newBridgeTestEnv(nil)
	route := testWatchRoute()

	env.hub.SetWatch(route, true)
	select {
	case routes := <-env.persisted:
		if len(routes) != 1 || routes[0].Key() != route.Key() {
			t.Fatalf("persisted = %+v, want the subscribed route", routes)
		}
	case <-time.After(time.Second):
		t.Fatal("SetWatch did not persist watchers")
	}

	// 模拟重启:全新 hub 从配置种子恢复。
	env2 := newBridgeTestEnv(nil)
	env2.hub.seedWatchers([]bot.DesktopWatchRoute{route}, env2.hub.watcherVersion())
	if !env2.hub.Watching(route) {
		t.Fatal("seeded hub should be watching the persisted route")
	}
	env2.hub.observe("tab-x", event.Event{Kind: event.TurnDone})
	if call := env2.waitNotification(t); !strings.Contains(call.msg.Text, "✅") {
		t.Fatalf("seeded watcher did not receive notifications: %q", call.msg.Text)
	}
}

func TestBridgeSeedDoesNotOverwriteNewerRuntimeWatch(t *testing.T) {
	env := newBridgeTestEnv(nil)
	route := testWatchRoute()
	staleVersion := env.hub.watcherVersion()
	if err := env.hub.SetWatch(route, true); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	<-env.persisted

	// Simulate a runtime refresh carrying a config snapshot loaded before the
	// watch command persisted. It must not erase the newer in-process route.
	env.hub.seedWatchers(nil, staleVersion)
	if !env.hub.Watching(route) {
		t.Fatal("stale config seed overwrote the newer runtime subscription")
	}
}

func TestBridgeSeedPreservesWatchAfterPersistFailure(t *testing.T) {
	env := newBridgeTestEnv(nil)
	env.persistErr = errors.New("disk unavailable")
	route := testWatchRoute()
	if err := env.hub.SetWatch(route, true); err == nil {
		t.Fatal("SetWatch should report the persistence failure")
	}
	<-env.persisted

	env.hub.seedWatchers(nil, env.hub.watcherVersion())
	if !env.hub.Watching(route) {
		t.Fatal("disk snapshot erased a runtime watch whose persistence failed")
	}
}

func TestBridgeSeedAppliesFreshExternalConfig(t *testing.T) {
	env := newBridgeTestEnv(nil)
	route := testWatchRoute()
	version := env.hub.watcherVersion()
	env.hub.seedWatchers([]bot.DesktopWatchRoute{route}, version)
	if !env.hub.Watching(route) {
		t.Fatal("initial config seed did not apply")
	}

	env.hub.seedWatchers(nil, version)
	if env.hub.Watching(route) {
		t.Fatal("fresh external config update did not replace the watcher set")
	}
}

func TestBridgeApprovalRoutesToDetachedSession(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{
		{TabID: "tab-bg", Label: "后台任务", Detached: true, Ready: true},
	})

	env.hub.observe("tab-bg", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "appr-bg", Tool: "bash"}})
	if _, err := env.hub.Approve("appr-bg", true); err != nil {
		t.Fatalf("Approve on detached session: %v", err)
	}
	select {
	case got := <-env.approves:
		if got[0] != "tab-bg" {
			t.Fatalf("approve routed to %v, want tab-bg", got)
		}
	case <-time.After(time.Second):
		t.Fatal("detached approval was not routed")
	}
}

func TestBridgeTakeoverLifecycle(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{
		{TabID: "tab-1", Label: "会话一", Ready: true},
		{TabID: "tab-bg", Label: "后台", Detached: true},
	})
	route := testWatchRoute()

	// 后台会话拒绝接管。
	if _, err := env.hub.Takeover(route, "tab-bg"); err == nil {
		t.Fatal("takeover of a detached session should fail")
	}

	feedback, err := env.hub.Takeover(route, "tab-1")
	if err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if !strings.Contains(feedback, "已接管") {
		t.Fatalf("feedback = %q", feedback)
	}
	if env.hub.TakeoverTab(route) != "tab-1" {
		t.Fatalf("TakeoverTab = %q, want tab-1", env.hub.TakeoverTab(route))
	}
	select {
	case got := <-env.announced:
		if got[0] != "tab-1" || !strings.Contains(got[1], "接管") {
			t.Fatalf("announce = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("takeover was not announced to the desktop transcript")
	}

	// 驱动输入路由到 tab。
	if _, err := env.hub.DriveInput(route, "跑一下测试"); err != nil {
		t.Fatalf("DriveInput: %v", err)
	}
	select {
	case got := <-env.driven:
		if got[0] != "tab-1" || got[1] != "跑一下测试" {
			t.Fatalf("driven = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("drive input was not routed")
	}

	// 另一个聊天抢同一会话被拒。
	other := route
	other.ChatID = "chat-other"
	if _, err := env.hub.Takeover(other, "tab-1"); err == nil {
		t.Fatal("takeover by another chat should be rejected while held")
	}

	// 释放。
	if _, err := env.hub.Release(route); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if env.hub.TakeoverTab(route) != "" {
		t.Fatal("binding should be cleared after release")
	}
	if _, err := env.hub.Release(route); err == nil {
		t.Fatal("second release should report no binding")
	}
}

func TestBridgeDriveInputRejectsRunningSession(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{
		{TabID: "tab-1", Label: "会话一", Ready: true, Running: true},
	})
	route := testWatchRoute()
	if _, err := env.hub.Takeover(route, "tab-1"); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	<-env.announced
	if _, err := env.hub.DriveInput(route, "hello"); err == nil || !strings.Contains(err.Error(), "正在执行中") {
		t.Fatalf("DriveInput on running session = %v, want busy rejection", err)
	}
}

func TestBridgeReclaimFromDesktopNotifiesController(t *testing.T) {
	env := newBridgeTestEnvSessions([]bot.DesktopSessionInfo{
		{TabID: "tab-1", Label: "会话一", Ready: true},
	})
	route := testWatchRoute()
	if _, err := env.hub.Takeover(route, "tab-1"); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	<-env.announced

	env.hub.reclaimFromDesktop("tab-1")
	if env.hub.TakeoverTab(route) != "" {
		t.Fatal("reclaim should clear the binding")
	}
	call := env.waitNotification(t)
	if !strings.Contains(call.msg.Text, "收回") || call.msg.ChatID != route.ChatID {
		t.Fatalf("reclaim notification = %+v", call)
	}

	// 未接管 tab 的 reclaim 是 no-op。
	env.hub.reclaimFromDesktop("tab-1")
	env.expectNoNotification(t)
}
