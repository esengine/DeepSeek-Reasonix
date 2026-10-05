package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type posture struct {
	mode string
	plan bool
}

// settle runs cmd against the kernel and then answers /status the way the
// kernel would have, since the recording kernel reports nothing back.
func settle(m *model, cmd tea.Cmd) posture {
	p := posture{m.status.ToolApprovalMode, m.status.Plan}
	run(m, cmd)
	m.status.ToolApprovalMode, m.status.Plan = p.mode, p.plan
	return p
}

func TestShiftTabCyclesInTheOrder1xUsed(t *testing.T) {
	m, k := testModel(t)
	m.status.ToolApprovalMode = "readOnly"
	want := []posture{{"ask", false}, {"auto", false}, {"yolo", false}, {"ask", true}, {"readOnly", false}, {"ask", false}}
	for i, w := range want {
		if got := settle(m, m.cycleMode()); got != w {
			t.Fatalf("step %d: Shift+Tab gave %+v, want %+v", i, got, w)
		}
	}
	calls := strings.Join(k.seen(), "\n")
	for _, c := range []string{`/tool-approval-mode {"mode":"readOnly"}`, `/plan {"on":true}`, `/plan {"on":false}`, `/tool-approval-mode {"mode":"yolo"}`} {
		if !strings.Contains(calls, c) {
			t.Errorf("the kernel never saw %s:\n%s", c, calls)
		}
	}
}

func TestShiftTabAlwaysHasYoloOnTheCycle(t *testing.T) {
	m, _ := testModel(t)
	m.status.ToolApprovalMode = "auto"
	if got := settle(m, m.cycleMode()); got != (posture{"yolo", false}) {
		t.Fatalf("auto steps to YOLO with no earlier confirmation: got %+v", got)
	}
}

func TestCtrlYTakesEffectOnOnePressAndReturnsToThePostureItLeft(t *testing.T) {
	m, k := testModel(t)
	for _, from := range []string{"auto", "readOnly"} {
		m.status.ToolApprovalMode = from
		pressedAndHeld(m, m.toggleYolo())
		if m.status.ToolApprovalMode != "yolo" {
			t.Fatalf("one Ctrl+Y from %s must enter YOLO, got %q", from, m.status.ToolApprovalMode)
		}
		pressedAndHeld(m, m.toggleYolo())
		if m.status.ToolApprovalMode != from {
			t.Fatalf("leaving YOLO goes back to %s, got %q", from, m.status.ToolApprovalMode)
		}
	}
	if !strings.Contains(strings.Join(k.seen(), "\n"), `/tool-approval-mode {"mode":"yolo"}`) {
		t.Fatalf("Ctrl+Y never reached the kernel as yolo:\n%s", strings.Join(k.seen(), "\n"))
	}
	if strings.TrimSpace(noticeText(m)) != "" {
		t.Fatalf("Ctrl+Y asks nothing and says nothing: %q", noticeText(m))
	}
}

func TestCtrlYKeyEntersYoloAtOnceAndTheFooterSaysSo(t *testing.T) {
	m, k := testModel(t)
	m.status.ToolApprovalMode = "auto"
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	run(m, cmd)
	if !strings.Contains(strings.Join(k.seen(), "\n"), `/tool-approval-mode {"mode":"yolo"}`) {
		t.Fatalf("the Ctrl+Y key never reached the kernel as yolo:\n%s", strings.Join(k.seen(), "\n"))
	}
	m.status.ToolApprovalMode = "yolo"
	if !strings.Contains(m.modeTag(), "YOLO") {
		t.Fatalf("footer tag = %q", m.modeTag())
	}
}

func TestFooterNamesReadOnlyAndDontAskApart(t *testing.T) {
	m, _ := testModel(t)
	m.status.ToolApprovalMode = "readOnly"
	if tag := m.modeTag(); !strings.Contains(tag, "Read only") {
		t.Fatalf("read-only footer tag = %q", tag)
	}
	m.status.ToolApprovalMode = "dontAsk"
	if tag := m.modeTag(); strings.Contains(tag, "Read only") {
		t.Fatalf("dontAsk still reads as read only: %q", tag)
	}
}

// pressedAndHeld runs cmd against the kernel and keeps the posture the model showed
// straight after the key, since the recording kernel's /status answers empty.
func pressedAndHeld(m *model, cmd tea.Cmd) {
	mode, plan := m.status.ToolApprovalMode, m.status.Plan
	run(m, cmd)
	m.status.ToolApprovalMode, m.status.Plan = mode, plan
}

func noticeText(m *model) string {
	var b strings.Builder
	for _, it := range m.tr.Items {
		b.WriteString(it.Text)
		b.WriteString("\n")
	}
	return b.String()
}
