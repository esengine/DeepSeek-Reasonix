package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestApprovalCardCarriesChatType(t *testing.T) {
	card := approvalCard(event.Approval{ID: "approval-1"}, ChatDM, "allowed-user")
	if len(card.Elements) < 2 {
		t.Fatalf("approval card elements = %d, want at least 2", len(card.Elements))
	}
	actions, ok := card.Elements[1].Extra["actions"].([]map[string]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("approval card actions missing or wrong type: %#v", card.Elements[1].Extra["actions"])
	}
	value, ok := actions[0]["value"].(map[string]string)
	if !ok {
		t.Fatalf("approval action value has wrong type: %#v", actions[0]["value"])
	}
	if value["command"] != "/approve approval-1" {
		t.Fatalf("command = %q, want /approve approval-1", value["command"])
	}
	if value["chat_type"] != string(ChatDM) {
		t.Fatalf("chat_type = %q, want %q", value["chat_type"], ChatDM)
	}
	if value["user_id"] != "allowed-user" {
		t.Fatalf("user_id = %q, want allowed-user", value["user_id"])
	}
}

func TestApprovalCardActionsAreToolAgnostic(t *testing.T) {
	for _, approval := range []event.Approval{
		{ID: "plan-1", Tool: "exit_plan_mode", Subject: "plan"},
		{ID: "task-1", Tool: "task", Subject: "run subtask"},
	} {
		card := approvalCard(approval, ChatGroup, "allowed-user")
		if len(card.Elements) < 2 {
			t.Fatalf("%s card elements = %d, want actions", approval.Tool, len(card.Elements))
		}
		actions, ok := card.Elements[1].Extra["actions"].([]map[string]any)
		if !ok || len(actions) != 2 {
			t.Fatalf("%s actions missing or wrong type: %#v", approval.Tool, card.Elements[1].Extra["actions"])
		}
		allow, ok := actions[0]["value"].(map[string]string)
		if !ok {
			t.Fatalf("%s allow value has wrong type: %#v", approval.Tool, actions[0]["value"])
		}
		deny, ok := actions[1]["value"].(map[string]string)
		if !ok {
			t.Fatalf("%s deny value has wrong type: %#v", approval.Tool, actions[1]["value"])
		}
		if allow["command"] != "/approve "+approval.ID || deny["command"] != "/deny "+approval.ID {
			t.Fatalf("%s commands = %q/%q, want approve/deny by id", approval.Tool, allow["command"], deny["command"])
		}
	}
}

func TestAskCardAddsAnswerButtonsForSingleChoice(t *testing.T) {
	card := askCard(event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:     "q1",
			Prompt: "Choose one",
			Options: []event.AskOption{
				{Label: "允许一次"},
				{Label: "拒绝"},
			},
		}},
	}, "fallback", ChatDM, "allowed-user")

	if len(card.Elements) != 2 {
		t.Fatalf("ask card elements = %d, want markdown + actions", len(card.Elements))
	}
	actions, ok := card.Elements[1].Extra["actions"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("ask card actions missing or wrong type: %#v", card.Elements[1].Extra["actions"])
	}
	value, ok := actions[0]["value"].(map[string]string)
	if !ok {
		t.Fatalf("ask action value has wrong type: %#v", actions[0]["value"])
	}
	if value["command"] != "/answer ask-1 1" {
		t.Fatalf("command = %q, want /answer ask-1 1", value["command"])
	}
	if value["chat_type"] != string(ChatDM) {
		t.Fatalf("chat_type = %q, want %q", value["chat_type"], ChatDM)
	}
	if value["user_id"] != "allowed-user" {
		t.Fatalf("user_id = %q, want allowed-user", value["user_id"])
	}
}

func TestRenderSinkDoesNotFlushMidSentenceOnTimer(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	sink.lastFlush = time.Now().Add(-2 * time.Second)

	sink.Emit(event.Event{Kind: event.Text, Text: "我是 **"})
	sink.Emit(event.Event{Kind: event.Text, Text: "Reasonix**，一个专注于执行代码任务的 AI 编程助手"})

	if sent := adapter.sentMessages(); len(sent) != 0 {
		t.Fatalf("sent = %+v, want no mid-sentence flush", sent)
	}

	sink.Emit(event.Event{Kind: event.TurnDone})
	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want final flush only", len(sent))
	}
	if sent[0].Text != "我是 **Reasonix**，一个专注于执行代码任务的 AI 编程助手" {
		t.Fatalf("sent text = %q, want combined sentence", sent[0].Text)
	}
}

func TestRenderSinkKeepsSemanticTextUntilFinalResult(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	sink.lastFlush = time.Now().Add(-2 * time.Second)

	sink.Emit(event.Event{Kind: event.Text, Text: "第一句。"})

	if sent := adapter.sentMessages(); len(sent) != 0 {
		t.Fatalf("sent = %+v, want semantic text held until final result", sent)
	}

	sink.Emit(event.Event{Kind: event.TurnDone})
	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want final result only", len(sent))
	}
	if sent[0].Text != "第一句。" {
		t.Fatalf("sent text = %q, want final result", sent[0].Text)
	}
}

func TestRenderSinkFinalFlushKeepsChunkLimit(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	sink.buf.WriteString(strings.Repeat("长", renderMaxChunkRunes*2+10))

	sink.Emit(event.Event{Kind: event.TurnDone})

	sent := adapter.sentMessages()
	if len(sent) < 2 {
		t.Fatalf("sent count = %d, want chunked final flush", len(sent))
	}
	for i, msg := range sent {
		if got := len([]rune(msg.Text)); got > renderMaxChunkRunes {
			t.Fatalf("sent[%d] runes = %d, want <= %d", i, got, renderMaxChunkRunes)
		}
	}
}

func TestRenderSinkConsumesEmptyWhitespacePrefix(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	sink.buf.WriteString("\n工具状态")

	sink.flushPrefix(1)

	if got := sink.buf.String(); got != "工具状态" {
		t.Fatalf("buffer = %q, want leading newline consumed", got)
	}
	if sent := adapter.sentMessages(); len(sent) != 0 {
		t.Fatalf("sent = %+v, want no empty outbound message", sent)
	}
}

func TestRenderSinkSendsProgressWithoutToolOutput(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "tool-1", Name: "read_file", ReadOnly: true}})
	sink.lastProgress = time.Now().Add(-renderProgressMinInterval)
	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "tool-1", Name: "read_file", ReadOnly: true, Refreshed: true}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "tool-1", Name: "read_file", Output: "secret output that should stay out of IM"}})
	sink.Emit(event.Event{Kind: event.Text, Text: "完成。"})
	sink.Emit(event.Event{Kind: event.TurnDone})

	sent := adapter.sentMessages()
	if len(sent) != 2 {
		t.Fatalf("sent count = %d, want one progress message plus final result: %+v", len(sent), sent)
	}
	if sent[0].Text != "正在执行: read_file" {
		t.Fatalf("progress text = %q, want concise tool status", sent[0].Text)
	}
	if strings.Contains(sent[0].Text, "secret output") || strings.Contains(sent[1].Text, "secret output") {
		t.Fatalf("tool output leaked into IM messages: %+v", sent)
	}
	if sent[1].Text != "完成。" {
		t.Fatalf("final text = %q, want final result only", sent[1].Text)
	}
}

func TestRenderSinkLimitsProgressMessages(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	for i := 0; i < renderMaxProgressMessages+2; i++ {
		sink.lastProgress = time.Now().Add(-renderProgressMinInterval)
		sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "tool", Name: "bash"}})
	}

	sent := adapter.sentMessages()
	if len(sent) != renderMaxProgressMessages {
		t.Fatalf("sent count = %d, want capped progress count %d", len(sent), renderMaxProgressMessages)
	}
}

type fakeEditorAdapter struct {
	*fakeAdapter
	mu      sync.Mutex
	edits   []editRecord
	editErr error
}

type editRecord struct {
	messageID string
	text      string
}

func newFakeEditorAdapter() *fakeEditorAdapter {
	return &fakeEditorAdapter{fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu")}
}

func (f *fakeEditorAdapter) EditMessage(ctx context.Context, messageID string, msg OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editErr != nil {
		return f.editErr
	}
	f.edits = append(f.edits, editRecord{messageID: messageID, text: msg.Text})
	return nil
}

func (f *fakeEditorAdapter) editRecords() []editRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]editRecord, len(f.edits))
	copy(out, f.edits)
	return out
}

func TestRenderSinkStreamsIntoLiveMessage(t *testing.T) {
	adapter := newFakeEditorAdapter()
	sink := newRenderSink(context.Background(), adapter, "feishu-feishu", "feishu", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	// 第一个增量：超过软窗口后创建 live 消息。
	sink.lastFlush = time.Now().Add(-2 * renderSoftFlushAfter)
	sink.Emit(event.Event{Kind: event.Text, Text: "第一段内容"})
	if sent := adapter.sentMessages(); len(sent) != 1 || sent[0].Text != "第一段内容" {
		t.Fatalf("sent = %+v, want live message created with first chunk", sent)
	}
	if sink.liveMsgID != "fake_msg_1" {
		t.Fatalf("liveMsgID = %q, want fake_msg_1", sink.liveMsgID)
	}

	// 第二个增量：原地编辑同一条消息，而不是再发一条。
	sink.lastEdit = time.Now().Add(-2 * renderSoftFlushAfter)
	sink.Emit(event.Event{Kind: event.Text, Text: "，第二段内容。"})
	if sent := adapter.sentMessages(); len(sent) != 1 {
		t.Fatalf("sent count = %d, want still one message after streaming edit", len(sent))
	}
	edits := adapter.editRecords()
	if len(edits) != 1 || edits[0].messageID != "fake_msg_1" {
		t.Fatalf("edits = %+v, want one edit to live message", edits)
	}
	if edits[0].text != "第一段内容，第二段内容。" {
		t.Fatalf("edit text = %q, want cumulative content", edits[0].text)
	}

	// 回合结束：最终内容编辑进 live 消息，不再新发。
	sink.Emit(event.Event{Kind: event.Text, Text: "收尾。"})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if sent := adapter.sentMessages(); len(sent) != 1 {
		t.Fatalf("sent count = %d, want no extra message at turn end", len(sent))
	}
	edits = adapter.editRecords()
	final := edits[len(edits)-1]
	if final.text != "第一段内容，第二段内容。收尾。" {
		t.Fatalf("final edit = %q, want full content", final.text)
	}
	if sink.liveMsgID != "" {
		t.Fatalf("liveMsgID = %q, want cleared after turn done", sink.liveMsgID)
	}
}

func TestRenderSinkStreamingThrottledBySoftWindow(t *testing.T) {
	adapter := newFakeEditorAdapter()
	sink := newRenderSink(context.Background(), adapter, "feishu-feishu", "feishu", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	// 软窗口内的增量不触发任何网络调用。
	sink.Emit(event.Event{Kind: event.Text, Text: "刚开始的内容"})
	if sent := adapter.sentMessages(); len(sent) != 0 {
		t.Fatalf("sent = %+v, want throttled inside soft window", sent)
	}
	if edits := adapter.editRecords(); len(edits) != 0 {
		t.Fatalf("edits = %+v, want none inside soft window", edits)
	}
}

func TestRenderSinkStreamingEditFailureRotatesWithoutDuplication(t *testing.T) {
	adapter := newFakeEditorAdapter()
	sink := newRenderSink(context.Background(), adapter, "feishu-feishu", "feishu", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	sink.lastFlush = time.Now().Add(-2 * renderSoftFlushAfter)
	sink.Emit(event.Event{Kind: event.Text, Text: "已送达的内容。"})
	if sink.liveMsgID == "" {
		t.Fatal("live message should be created")
	}

	// 编辑失败：块轮转，已送达前缀不重发。
	adapter.mu.Lock()
	adapter.editErr = context.DeadlineExceeded
	adapter.mu.Unlock()
	sink.lastEdit = time.Now().Add(-2 * renderSoftFlushAfter)
	sink.Emit(event.Event{Kind: event.Text, Text: "后续内容。"})
	if sink.liveMsgID != "" {
		t.Fatalf("liveMsgID = %q, want rotation after edit failure", sink.liveMsgID)
	}

	adapter.mu.Lock()
	adapter.editErr = nil
	adapter.mu.Unlock()
	sink.Emit(event.Event{Kind: event.TurnDone})
	sent := adapter.sentMessages()
	if len(sent) != 2 {
		t.Fatalf("sent count = %d, want live message plus rotated tail: %+v", len(sent), sent)
	}
	if sent[1].Text != "后续内容。" {
		t.Fatalf("rotated tail = %q, want only undelivered content", sent[1].Text)
	}
}

func TestRenderSinkStreamingHardCapRotatesBlocks(t *testing.T) {
	adapter := newFakeEditorAdapter()
	sink := newRenderSink(context.Background(), adapter, "feishu-feishu", "feishu", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	sink.lastFlush = time.Now().Add(-2 * renderSoftFlushAfter)
	sink.Emit(event.Event{Kind: event.Text, Text: "第一句。"})
	if sink.liveMsgID == "" {
		t.Fatal("live message should be created")
	}

	// 超过硬上限：live 消息按语义边界收尾，剩余进入下一块。
	sink.Emit(event.Event{Kind: event.Text, Text: strings.Repeat("长", renderHardChunkRunes) + "。尾部"})
	if sink.liveMsgID != "" {
		t.Fatalf("liveMsgID = %q, want block closed at hard cap", sink.liveMsgID)
	}
	edits := adapter.editRecords()
	if len(edits) == 0 {
		t.Fatal("hard cap should finalize the live message via edit")
	}
	if got := len([]rune(edits[len(edits)-1].text)); got > renderMaxChunkRunes {
		t.Fatalf("finalized block runes = %d, want <= %d", got, renderMaxChunkRunes)
	}

	sink.Emit(event.Event{Kind: event.TurnDone})
	sent := adapter.sentMessages()
	if len(sent) < 2 {
		t.Fatalf("sent count = %d, want new message for the next block", len(sent))
	}
}

// failingEditorAdapter accepts the initial Send (returns a message id so
// streaming engages) but fails every edit, simulating a rate-limited / recalled
// live message mid-turn.
type failingEditorAdapter struct {
	*fakeAdapter
}

func (f *failingEditorAdapter) EditMessage(ctx context.Context, id string, msg OutboundMessage) error {
	return fmt.Errorf("simulated edit failure")
}

func TestRenderSinkStreamingEditFailureDoesNotDuplicate(t *testing.T) {
	adapter := &failingEditorAdapter{fakeAdapter: newFakeAdapter(PlatformFeishu, "fake-feishu")}
	sink := newRenderSink(context.Background(), adapter, "feishu-feishu", "feishu", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	// Stream a first chunk so a live message is created (liveSentBytes == full).
	sink.lastFlush = time.Now().Add(-2 * renderSoftFlushAfter)
	head := strings.Repeat("a", 2000)
	sink.Emit(event.Event{Kind: event.Text, Text: head})
	if sink.liveMsgID == "" {
		t.Fatalf("streaming did not engage; sent=%d", len(adapter.sentMessages()))
	}
	// Push past the hard cap so flushPrefix runs with idx < liveSentBytes, then
	// finish. Every edit fails, so rotation must not re-queue already-shown text.
	tail := strings.Repeat("b", renderHardChunkRunes)
	sink.Emit(event.Event{Kind: event.Text, Text: tail})
	sink.Emit(event.Event{Kind: event.TurnDone})

	var shown strings.Builder
	shown.WriteString(head) // live message frozen at last successful state
	for _, m := range adapter.sentMessages()[1:] {
		shown.WriteString(m.Text)
	}
	wantRunes := len([]rune(head + tail))
	if gotRunes := len([]rune(shown.String())); gotRunes > wantRunes {
		t.Fatalf("duplication: user would see %d runes, expected at most %d (%d duplicated)", gotRunes, wantRunes, gotRunes-wantRunes)
	}
}

func TestRenderSinkStreamingFinalizesInOneEditWithoutSplit(t *testing.T) {
	adapter := newFakeEditorAdapter()
	sink := newRenderSink(context.Background(), adapter, "feishu-feishu", "feishu", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	// A final answer that does NOT end on a semantic boundary (ends inside a
	// code fence) must be finalized as a single in-place edit, not shrunk +
	// tail-as-new-message.
	sink.lastFlush = time.Now().Add(-2 * renderSoftFlushAfter)
	sink.Emit(event.Event{Kind: event.Text, Text: "结论如下。这里是代码:\n```go\nfmt.Println(\"x\")\n```"})
	sink.Emit(event.Event{Kind: event.TurnDone})

	if sent := adapter.sentMessages(); len(sent) != 1 {
		t.Fatalf("sent %d messages, want exactly one live message (no split): %+v", len(sent), sent)
	}
	edits := adapter.editRecords()
	if len(edits) == 0 || !strings.Contains(edits[len(edits)-1].text, "```") {
		t.Fatalf("final edit should carry the full text including the code fence: %+v", edits)
	}
}

func TestRenderSinkSuppressesReasoning(t *testing.T) {
	adapter := newFakeAdapter(PlatformWeixin, "fake-weixin")
	sink := newRenderSink(context.Background(), adapter, "weixin-weixin", "weixin", "chat-1", ChatDM, "user-1", "msg-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	sink.Emit(event.Event{Kind: event.Reasoning, Text: "internal reasoning"})
	sink.Emit(event.Event{Kind: event.Text, Text: "可见结果"})
	sink.Emit(event.Event{Kind: event.TurnDone})

	sent := adapter.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent count = %d, want one final result", len(sent))
	}
	if strings.Contains(sent[0].Text, "internal reasoning") {
		t.Fatalf("reasoning leaked into IM message: %q", sent[0].Text)
	}
}
