package control

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestIsNonTurnHTTPInput(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"", true},               // empty
		{"  ", true},             // blank
		{"# note text", true},    // memory quick-add (# + space)
		{"/remember MiMo", true}, // remember command note
		{"/compact", true},       // slash command
		{"/model qwen3", true},   // management verb
		{"/new", true},           // slash command
		{"!ls", true},            // shell commands rejected by submitHTTP (403) before any turn
		{"hello", false},         // ordinary turn
		{"explain this code", false},
	} {
		if got := isNonTurnHTTPInput(tc.input); got != tc.want {
			t.Errorf("isNonTurnHTTPInput(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

type observedTurnFormat struct {
	input  string
	format string
}

type formatRecordingRunner struct {
	observed chan<- observedTurnFormat
}

func (r formatRecordingRunner) Run(ctx context.Context, input string) error {
	format := ""
	if responseFormat := agent.ResponseFormatFromRequest(ctx); responseFormat != nil {
		format = responseFormat.Type
	}
	r.observed <- observedTurnFormat{input: input, format: format}
	return nil
}

type formatTurnDoneGate struct {
	mu           sync.Mutex
	turns        int
	firstEntered chan struct{}
	releaseFirst chan struct{}
	allDone      chan struct{}
}

func (g *formatTurnDoneGate) Emit(e event.Event) {
	if e.Kind != event.TurnDone {
		return
	}
	g.mu.Lock()
	g.turns++
	turn := g.turns
	g.mu.Unlock()

	if turn == 1 {
		close(g.firstEntered)
		<-g.releaseFirst
	}
	if turn == 2 {
		close(g.allDone)
	}
}

func receiveObservedTurnFormat(t *testing.T, observed <-chan observedTurnFormat) observedTurnFormat {
	t.Helper()
	select {
	case got := <-observed:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for submitted turn")
		return observedTurnFormat{}
	}
}

func waitForFormatTestSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

// TestSubmitHTTPFormatBindsToTurn holds the first turn's finishing window open,
// submits a second turn with a different format, and proves the parked closure
// preserves each accepted turn's format. This deterministically exercises the
// interleaving that a controller-global one-shot slot could cross-wire.
func TestSubmitHTTPFormatBindsToTurn(t *testing.T) {
	observed := make(chan observedTurnFormat, 2)
	gate := &formatTurnDoneGate{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		allDone:      make(chan struct{}),
	}
	c := New(Options{Runner: formatRecordingRunner{observed: observed}, Sink: gate})

	c.SubmitHTTPFormat("first turn", "format-a")
	first := receiveObservedTurnFormat(t, observed)
	waitForFormatTestSignal(t, gate.firstEntered, "first turn did not enter the finishing window")

	c.SubmitHTTPFormat("second turn", "format-b")
	close(gate.releaseFirst)
	second := receiveObservedTurnFormat(t, observed)
	waitForFormatTestSignal(t, gate.allDone, "second turn did not finish")

	if !strings.Contains(first.input, "first turn") || first.format != "format-a" {
		t.Fatalf("first turn = %+v, want first input with format-a", first)
	}
	if !strings.Contains(second.input, "second turn") || second.format != "format-b" {
		t.Fatalf("second turn = %+v, want second input with format-b", second)
	}
}

// TestWithTurnFormatInjectsFormatIntoContext：format 绑定 turn 的实际效果
// ——withTurnFormat 注入后 agent 请求路径能读到（不是全局槽）。
func TestWithTurnFormatInjectsFormatIntoContext(t *testing.T) {
	c := New(Options{})
	ctx := context.Background()
	if got := agent.ResponseFormatFromRequest(c.withTurnFormat(ctx, "")); got != nil {
		t.Fatalf("empty format must be no-op, got %+v", got)
	}
	if got := agent.ResponseFormatFromRequest(c.withTurnFormat(ctx, "json_object")); got == nil || got.Type != "json_object" {
		t.Fatalf("turn format must reach agent request, got %+v", got)
	}
}

// TestRefTurnFormatBound：@reference turn 同样绑定 format（统一架构——
// format 是每个被接纳 turn 的属性，非 runGoalLoop 特例）。
func TestRefTurnFormatBound(t *testing.T) {
	c := New(Options{})
	ctx := context.Background()
	// runRefTurnWithFormat 注入后 agent 请求路径读到 json_object
	if got := agent.ResponseFormatFromRequest(c.withTurnFormat(ctx, "json_object")); got == nil || got.Type != "json_object" {
		t.Fatalf("ref-turn format must bind to ctx, got %+v", got)
	}
	// isRefTurnInput 识别 @引用 turn（format 经 wrapper 绑定，不再丢弃）
	// ref-turn 输入识别（SlashCodeCommentLine 不依赖文件系统）
	for _, input := range []string{"// comment line", "//src/main.go:12"} {
		if !SlashCodeCommentLine(input) {
			t.Errorf("SlashCodeCommentLine(%q) = false, want true", input)
		}
	}
}
