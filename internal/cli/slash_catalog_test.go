package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/skill"
)

func TestSlashCatalogCachesAcrossKeystrokes(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = make([]skill.Skill, 0, 50)
	for i := 0; i < 50; i++ {
		m.skills = append(m.skills, skill.Skill{
			Name:        fmt.Sprintf("skill-%03d", i),
			Description: strings.Repeat("description text for catalog build ", 20),
		})
	}
	m.commands = []command.Command{{Name: "custom-cmd", Description: "custom"}}

	first := m.slashItems()
	if len(first) < 50 {
		t.Fatalf("catalog size = %d, want at least 50 skills", len(first))
	}
	// Second call must reuse the same backing slice (immutable snapshot).
	second := m.slashItems()
	if &first[0] != &second[0] || len(first) != len(second) {
		t.Fatal("slashItems must return the cached catalog between keystrokes")
	}
	// Fingerprint change (skill list) invalidates.
	m.skills = append(m.skills, skill.Skill{Name: "skill-extra", Description: "extra"})
	m.invalidateSlashCatalog()
	third := m.slashItems()
	if len(third) != len(first)+1 {
		t.Fatalf("after skill add catalog = %d, want %d", len(third), len(first)+1)
	}
}

func TestCtrlDForwardDeletesWhenComposerNonEmpty(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	m.input.SetValue("hello")
	m.input.SetCursorColumn(0) // caret at start so forward-delete removes 'h'

	msg := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	if msg.String() != "ctrl+d" {
		t.Fatalf("synthetic key String() = %q, want ctrl+d", msg.String())
	}
	out, _ := m.Update(msg)
	m = out.(chatTUI)
	if got := m.input.Value(); got != "ello" {
		t.Fatalf("ctrl+d on non-empty = %q, want ello", got)
	}
	if m.state != tuiIdle {
		t.Fatalf("state = %v, want idle (must not quit)", m.state)
	}
}

func TestCtrlDQuitsWhenIdleAndEmpty(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	m.input.SetValue("")
	msg := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("ctrl+d on empty idle composer should request shutdown")
	}
}

func TestActiveAtTokenRespectsCursor(t *testing.T) {
	val := "see @foo and more"
	// Cursor after "@fo"
	cursor := strings.Index(val, "@fo") + len("@fo")
	at, tok, ok := activeAtToken(val, cursor)
	if !ok || at != strings.Index(val, "@") || tok != "fo" {
		t.Fatalf("activeAtToken mid-token = (%d,%q,%v), want @ offset and fo", at, tok, ok)
	}
	// Cursor before '@' (on the space): no active token
	before := strings.Index(val, "@")
	at, tok, ok = activeAtToken(val, before)
	if ok {
		t.Fatalf("cursor before @ should not open token, got (%d,%q,%v)", at, tok, ok)
	}
}

func TestShiftTabAndBacktabCycleMode(t *testing.T) {
	ctrl := control.New(control.Options{})
	// New controllers default to Ask; Shift+Tab cycles Ask→Auto→Plan.
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	if m.ctrl == nil {
		t.Fatal("expected controller")
	}
	before := m.ctrl.ToolApprovalMode()
	msg := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	s := msg.String()
	if s != "shift+tab" && s != "backtab" {
		t.Fatalf("KeyTab+ModShift String() = %q, want shift+tab or backtab", s)
	}
	out, _ := m.Update(msg)
	m = out.(chatTUI)
	if m.ctrl.ToolApprovalMode() == before && !m.planMode {
		t.Fatalf("%q did not cycle mode (still %v, plan=%v)", s, before, m.planMode)
	}
}

// BenchmarkSlashCompletionKeystroke measures filter+menu update with a large
// catalog (1000 skills). Cached catalog build should keep per-keystroke work
// well under a frame budget on a typical laptop.
func BenchmarkSlashCompletionKeystroke(b *testing.B) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = make([]skill.Skill, 0, 1000)
	for i := 0; i < 1000; i++ {
		m.skills = append(m.skills, skill.Skill{
			Name:        fmt.Sprintf("bench-skill-%04d", i),
			Description: "benchmark skill description " + strings.Repeat("x", 80),
		})
	}
	// Warm the catalog once.
	_ = m.slashItems()
	start := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.input.SetValue("/be")
		m.updateCompletion()
		if !m.completion.active {
			b.Fatal("expected completion menu")
		}
	}
	b.StopTimer()
	if b.N > 0 {
		per := time.Since(start) / time.Duration(b.N)
		// Soft gate for local CI laptops: 16ms frame budget.
		if per > 16*time.Millisecond {
			b.Logf("WARNING: slash completion keystroke p50-ish %v exceeds 16ms frame budget (n=%d)", per, b.N)
		}
	}
}
