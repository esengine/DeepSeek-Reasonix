package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
)

type fakeDesktopBridge struct {
	sessions  []DesktopSessionInfo
	watching  map[string]bool
	approved  []string
	denied    []string
	answered  map[string][]event.AskAnswer
	questions map[string][]event.AskQuestion
	takeovers map[string]string // routeKey -> tabID
	driven    []string
	driveErr  error
	watchErr  error
}

func newFakeDesktopBridge() *fakeDesktopBridge {
	return &fakeDesktopBridge{
		watching:  make(map[string]bool),
		answered:  make(map[string][]event.AskAnswer),
		questions: make(map[string][]event.AskQuestion),
		takeovers: make(map[string]string),
	}
}

func (f *fakeDesktopBridge) Sessions() []DesktopSessionInfo { return f.sessions }
func (f *fakeDesktopBridge) SetWatch(route DesktopWatchRoute, enable bool) error {
	f.watching[route.Key()] = enable
	return f.watchErr
}
func (f *fakeDesktopBridge) Watching(route DesktopWatchRoute) bool {
	return f.watching[route.Key()]
}
func (f *fakeDesktopBridge) Approve(id string, allow bool) (string, error) {
	if id == "gone" {
		return "", fmt.Errorf("未找到待处理的审批 %s", id)
	}
	if allow {
		f.approved = append(f.approved, id)
	} else {
		f.denied = append(f.denied, id)
	}
	return "已提交", nil
}
func (f *fakeDesktopBridge) AskQuestions(id string) ([]event.AskQuestion, bool) {
	qs, ok := f.questions[id]
	return qs, ok
}
func (f *fakeDesktopBridge) Answer(id string, answers []event.AskAnswer) (string, error) {
	f.answered[id] = answers
	return "已提交回答", nil
}

func (f *fakeDesktopBridge) Takeover(route DesktopWatchRoute, tabID string) (string, error) {
	for _, s := range f.sessions {
		if s.TabID == tabID {
			f.takeovers[route.Key()] = tabID
			return "已接管", nil
		}
	}
	return "", fmt.Errorf("未找到会话 %s", tabID)
}

func (f *fakeDesktopBridge) Release(route DesktopWatchRoute) (string, error) {
	if _, ok := f.takeovers[route.Key()]; !ok {
		return "", fmt.Errorf("本聊天当前没有接管任何桌面会话。")
	}
	delete(f.takeovers, route.Key())
	return "已解除接管", nil
}

func (f *fakeDesktopBridge) TakeoverTab(route DesktopWatchRoute) string {
	return f.takeovers[route.Key()]
}

func (f *fakeDesktopBridge) DriveInput(route DesktopWatchRoute, text string) (string, error) {
	if f.driveErr != nil {
		return "", f.driveErr
	}
	f.driven = append(f.driven, text)
	return "", nil
}

func desktopTestMessage(text string) InboundMessage {
	return InboundMessage{
		Platform:     PlatformFeishu,
		ConnectionID: "feishu-main",
		Domain:       "feishu",
		ChatType:     ChatDM,
		ChatID:       "chat-god",
		UserID:       "admin-user",
		Text:         text,
	}
}

func TestHandleDesktopCommandWithoutBridge(t *testing.T) {
	gw := &BotGateway{cfg: GatewayConfig{}}
	got := gw.handleDesktopCommand(desktopTestMessage("/desktop status"))
	if !strings.Contains(got, "未运行在桌面端进程内") {
		t.Fatalf("reply = %q, want standalone-mode notice", got)
	}
}

func TestHandleDesktopCommandStatusListsSessions(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.sessions = []DesktopSessionInfo{
		{TabID: "tab-1", Label: "修复登录", Workspace: "blade", Running: true, Ready: true},
		{TabID: "tab-2", Label: "", Topic: "周报", Ready: true, PendingPrompt: true},
	}
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}

	got := gw.handleDesktopCommand(desktopTestMessage("/desktop status"))
	for _, want := range []string{"2 个", "修复登录", "▶️ 执行中", "周报", "⚠️ 等待审批/回答", "tab-1", "blade"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status reply = %q, want it to contain %q", got, want)
		}
	}
}

func TestHandleDesktopCommandStatusListsPendingIDs(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.sessions = []DesktopSessionInfo{{
		TabID: "tab-1", Label: "修复登录", Ready: true, PendingPrompt: true,
		Pending: []DesktopPendingInfo{{ID: "appr-9", Kind: "approval", Tool: "bash"}},
	}}
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}
	got := gw.handleDesktopCommand(desktopTestMessage("/desktop status"))
	// The pending id must be visible so a user whose push was dropped can still
	// run /desktop approve <id>.
	if !strings.Contains(got, "appr-9") {
		t.Fatalf("status = %q, want it to list the pending approval id", got)
	}
}

func TestHandleDesktopCommandStatusEmpty(t *testing.T) {
	gw := &BotGateway{cfg: GatewayConfig{Desktop: newFakeDesktopBridge()}}
	got := gw.handleDesktopCommand(desktopTestMessage("/desktop"))
	if !strings.Contains(got, "没有 live 会话") {
		t.Fatalf("reply = %q, want empty-sessions notice", got)
	}
}

func TestHandleDesktopCommandWatchLifecycle(t *testing.T) {
	bridge := newFakeDesktopBridge()
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}
	msg := desktopTestMessage("/desktop watch on")

	got := gw.handleDesktopCommand(msg)
	if !strings.Contains(got, "已订阅") {
		t.Fatalf("watch on reply = %q", got)
	}
	route := desktopRouteFromMessage(msg)
	if !bridge.watching[route.Key()] {
		t.Fatal("watch on did not subscribe the message route")
	}

	msg.Text = "/desktop watch off"
	got = gw.handleDesktopCommand(msg)
	if !strings.Contains(got, "已退订") {
		t.Fatalf("watch off reply = %q", got)
	}
	if bridge.watching[route.Key()] {
		t.Fatal("watch off did not unsubscribe the message route")
	}
}

func TestHandleDesktopCommandWatchReportsPersistenceFailure(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.watchErr = fmt.Errorf("disk unavailable")
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}
	msg := desktopTestMessage("/desktop watch on")

	got := gw.handleDesktopCommand(msg)
	if !strings.Contains(got, "本次运行中订阅") || !strings.Contains(got, "保存订阅失败") {
		t.Fatalf("watch persistence failure reply = %q", got)
	}
	if !bridge.Watching(desktopRouteFromMessage(msg)) {
		t.Fatal("runtime subscription should remain active after persistence failure")
	}
}

func TestHandleDesktopCommandApproveAndDeny(t *testing.T) {
	bridge := newFakeDesktopBridge()
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}

	if got := gw.handleDesktopCommand(desktopTestMessage("/desktop approve appr-1")); !strings.Contains(got, "已提交") {
		t.Fatalf("approve reply = %q", got)
	}
	if got := gw.handleDesktopCommand(desktopTestMessage("/desktop deny appr-2")); !strings.Contains(got, "已提交") {
		t.Fatalf("deny reply = %q", got)
	}
	if len(bridge.approved) != 1 || bridge.approved[0] != "appr-1" {
		t.Fatalf("approved = %v, want [appr-1]", bridge.approved)
	}
	if len(bridge.denied) != 1 || bridge.denied[0] != "appr-2" {
		t.Fatalf("denied = %v, want [appr-2]", bridge.denied)
	}

	if got := gw.handleDesktopCommand(desktopTestMessage("/desktop approve gone")); !strings.Contains(got, "未找到") {
		t.Fatalf("missing-approval reply = %q", got)
	}
	if got := gw.handleDesktopCommand(desktopTestMessage("/desktop approve")); got != desktopCommandUsage {
		t.Fatalf("missing-arg reply = %q, want usage", got)
	}
}

func TestHandleDesktopCommandAnswerParsesSelection(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.questions["ask-1"] = []event.AskQuestion{{
		ID:      "q1",
		Prompt:  "选一个",
		Options: []event.AskOption{{Label: "方案 A"}, {Label: "方案 B"}},
	}}
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}

	got := gw.handleDesktopCommand(desktopTestMessage("/desktop answer ask-1 2"))
	if !strings.Contains(got, "已提交回答") {
		t.Fatalf("answer reply = %q", got)
	}
	answers := bridge.answered["ask-1"]
	if len(answers) != 1 || answers[0].QuestionID != "q1" {
		t.Fatalf("answers = %+v, want one answer for q1", answers)
	}
	if len(answers[0].Selected) != 1 || answers[0].Selected[0] != "方案 B" {
		t.Fatalf("selected = %v, want numeric index resolved to 方案 B", answers[0].Selected)
	}

	if got := gw.handleDesktopCommand(desktopTestMessage("/desktop answer ask-gone 1")); !strings.Contains(got, "未找到") {
		t.Fatalf("missing-ask reply = %q", got)
	}
}

func TestHandleDesktopCommandTakeoverAndRelease(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.sessions = []DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}}
	gw := &BotGateway{cfg: GatewayConfig{Desktop: bridge}}
	msg := desktopTestMessage("/desktop takeover tab-1")

	if got := gw.handleDesktopCommand(msg); !strings.Contains(got, "已接管") {
		t.Fatalf("takeover reply = %q", got)
	}
	route := desktopRouteFromMessage(msg)
	if bridge.takeovers[route.Key()] != "tab-1" {
		t.Fatalf("takeovers = %v, want route bound to tab-1", bridge.takeovers)
	}

	msg.Text = "/desktop release"
	if got := gw.handleDesktopCommand(msg); !strings.Contains(got, "已解除") {
		t.Fatalf("release reply = %q", got)
	}
	if got := gw.handleDesktopCommand(desktopTestMessage("/desktop takeover missing")); !strings.Contains(got, "未找到") {
		t.Fatalf("missing-tab reply = %q", got)
	}
}

func TestDivertToDesktopTakeover(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.sessions = []DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}}
	gw := &BotGateway{
		cfg:           GatewayConfig{Desktop: bridge},
		logger:        discardLogger(),
		adapterHealth: map[string]*AdapterHealthSnapshot{},
	}
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := desktopTestMessage("帮我跑一下测试")

	// 未接管:不分流。
	if gw.divertToDesktopTakeover(context.Background(), adapter, msg) {
		t.Fatal("message should not divert without a takeover binding")
	}

	route := desktopRouteFromMessage(msg)
	bridge.takeovers[route.Key()] = "tab-1"
	msg.Text = "/desktop status"
	if gw.divertToDesktopTakeover(context.Background(), adapter, msg) {
		t.Fatal("slash commands must remain in the bot command path during takeover")
	}
	msg.Text = "帮我跑一下测试"
	if !gw.divertToDesktopTakeover(context.Background(), adapter, msg) {
		t.Fatal("message should divert to the taken-over session")
	}
	if len(bridge.driven) != 1 || bridge.driven[0] != "帮我跑一下测试" {
		t.Fatalf("driven = %v, want the plain message text", bridge.driven)
	}

	// 驱动失败:错误文案回给用户。
	bridge.driveErr = fmt.Errorf("会话正在执行中")
	if !gw.divertToDesktopTakeover(context.Background(), adapter, msg) {
		t.Fatal("drive failure should still consume the message")
	}
	sent := adapter.sentMessages()
	if len(sent) == 0 || !strings.Contains(sent[len(sent)-1].Text, "正在执行中") {
		t.Fatalf("sent = %+v, want drive error relayed to the chat", sent)
	}
}

func TestDivertToDesktopTakeoverRevokesFormerAdmin(t *testing.T) {
	bridge := newFakeDesktopBridge()
	bridge.sessions = []DesktopSessionInfo{{TabID: "tab-1", Label: "会话一"}}
	gw := &BotGateway{
		cfg: GatewayConfig{
			Desktop: bridge,
			Allowlist: AllowlistConfig{Admins: map[Platform][]string{
				PlatformFeishu: {"current-admin"},
			}},
		},
		logger:        discardLogger(),
		adapterHealth: map[string]*AdapterHealthSnapshot{},
	}
	adapter := newFakeAdapter(PlatformFeishu, "fake-feishu")
	msg := desktopTestMessage("run tests")
	route := desktopRouteFromMessage(msg)
	bridge.takeovers[route.Key()] = "tab-1"

	if !gw.divertToDesktopTakeover(context.Background(), adapter, msg) {
		t.Fatal("revoked takeover message should be consumed with an explanation")
	}
	if bridge.TakeoverTab(route) != "" || len(bridge.driven) != 0 {
		t.Fatalf("revoked takeover remained active or drove input: tab=%q driven=%v", bridge.TakeoverTab(route), bridge.driven)
	}
	sent := adapter.sentMessages()
	if len(sent) == 0 || !strings.Contains(sent[len(sent)-1].Text, "不再具有") {
		t.Fatalf("sent = %+v, want admin-revocation explanation", sent)
	}
}
