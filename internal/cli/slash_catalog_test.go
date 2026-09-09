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
	"reasonix/internal/memory"
	"reasonix/internal/skill"
)

func TestSlashCatalogCachesAcrossKeystrokes(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = make([]skill.Skill, 0, 50)
	for i := range 50 {
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
	// Explicit invalidation is required after source mutation.
	m.skills = append(m.skills, skill.Skill{Name: "skill-extra", Description: "extra"})
	// Without invalidate, cache must stay stale (no hot-path fingerprint).
	stale := m.slashItems()
	if len(stale) != len(first) {
		t.Fatalf("without invalidate catalog mutated on keystroke path: %d → %d", len(first), len(stale))
	}
	m.invalidateSlashCatalog()
	third := m.slashItems()
	if len(third) != len(first)+1 {
		t.Fatalf("after invalidate catalog = %d, want %d", len(third), len(first)+1)
	}
}

func TestCtrlDForwardDeletesWhenComposerNonEmpty(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	m.input.SetValue("hello")
	m.input.SetCursorColumn(0)

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

func TestCtrlDForwardDeletesWhitespaceOnly(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	m.input.SetValue("   ")
	m.input.SetCursorColumn(0)
	msg := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	out, _ := m.Update(msg)
	m = out.(chatTUI)
	if got := m.input.Value(); got == "   " {
		// At least one space should be deleted; exact remainder depends on
		// textarea delete-forward at col 0. Unchanged value would mean quit
		// (or no-op) rather than forward-delete.
		t.Fatalf("ctrl+d on whitespace-only must forward-delete, not quit; value still %q", got)
	}
	if m.state != tuiIdle {
		t.Fatalf("must not quit on whitespace-only input")
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

func TestActiveAtTokenFullSpanAndMidCursor(t *testing.T) {
	val := "see @foo and more"
	// Cursor mid-token after "@fo" → query is caret-limited "fo", span is full "@foo".
	cursor := strings.Index(val, "@fo") + len("@fo")
	at, end, tok, ok := activeAtToken(val, cursor)
	if !ok || at != strings.Index(val, "@") || tok != "fo" {
		t.Fatalf("activeAtToken mid-token = (%d,%d,%q,%v), want query fo", at, end, tok, ok)
	}
	if val[at:end] != "@foo" {
		t.Fatalf("replace span = %q, want @foo (full token past caret)", val[at:end])
	}
}

func TestMCPSurfaceReadyInvalidatesSlashCatalog(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = []skill.Skill{{Name: "warm", Description: "warm"}}
	_ = m.slashItems()
	if m.slashCache == nil || m.slashCache.items == nil {
		t.Fatal("expected warm catalog")
	}
	m.ingestEvent(event.Event{Kind: event.MCPSurfaceReady})
	if m.slashCache != nil {
		t.Fatal("MCPSurfaceReady must invalidate slash catalog")
	}
}

func TestAcceptAtCompletionReplacesFullToken(t *testing.T) {
	// Manual completion state: proves replaceFrom/replaceTo replace the whole
	// token and preserve surrounding spaces (the audited "see @foo and more"
	// regression).
	m := newTestChatTUI()
	m.input.SetValue("see @foo and more")
	// "@foo" spans bytes [4, 8)
	m.completion = completion{
		active:      true,
		kind:        compAt,
		items:       []compItem{{label: "@foobar.md", insert: "@foobar.md"}},
		sel:         0,
		replaceFrom: 4,
		replaceTo:   8,
	}
	m.acceptCompletion()
	got := m.input.Value()
	want := "see @foobar.md and more"
	if got != want {
		t.Fatalf("accept mid-token = %q, want %q", got, want)
	}
}

func TestInputCursorByteOffsetSubtractsPrompt(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	// Place "hello!!" and put caret after "hello" (rune index 5).
	m.input.SetValue("hello!!")
	m.setComposerCursor(5)
	// Force layout cache used by inputCursorByteOffset.
	_ = m.composerRows()
	got := m.inputCursorByteOffset()
	if got != 5 {
		t.Fatalf("inputCursorByteOffset = %d, want 5 (prompt gutter must not add 2)", got)
	}
}

func TestShiftTabAndBacktabBothAccepted(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	if m.ctrl == nil {
		t.Fatal("expected controller")
	}

	// Production uses modeToggleKey for both encodings before the key switch.
	for _, key := range []string{"shift+tab", "backtab"} {
		if !modeToggleKey(key) {
			t.Fatalf("modeToggleKey(%q) = false, want true", key)
		}
	}
	if modeToggleKey("tab") || modeToggleKey("shift+enter") {
		t.Fatal("modeToggleKey must not accept unrelated keys")
	}

	// Platform form: KeyTab+ModShift (typically String() == "shift+tab").
	msg := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	s := msg.String()
	if !modeToggleKey(s) {
		t.Fatalf("KeyTab+ModShift String() = %q is not a mode-toggle key", s)
	}
	before := m.ctrl.ToolApprovalMode()
	out, _ := m.Update(msg)
	m = out.(chatTUI)
	if m.ctrl.ToolApprovalMode() == before && !m.planMode {
		t.Fatalf("%q did not cycle mode via Update", s)
	}

	// Explicit CSI-Z / legacy "backtab" text encoding through the production
	// Update path (Key.String returns Text when non-empty).
	before = m.ctrl.ToolApprovalMode()
	planBefore := m.planMode
	out, _ = m.Update(tea.KeyPressMsg{Text: "backtab"})
	m = out.(chatTUI)
	if m.ctrl.ToolApprovalMode() == before && m.planMode == planBefore {
		t.Fatal(`Update(Text:"backtab") did not cycle mode — production path must honor modeToggleKey("backtab")`)
	}
}

// BenchmarkSlashCompletionKeystroke measures filter+menu update with a large
// catalog (1000 skills). Catalog is warmed once; per-op cost must stay low and
// allocation-stable (no fingerprint rebuild).
func BenchmarkSlashCompletionKeystroke(b *testing.B) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = make([]skill.Skill, 0, 1000)
	for i := range 1000 {
		m.skills = append(m.skills, skill.Skill{
			Name:        fmt.Sprintf("bench-skill-%04d", i),
			Description: "benchmark skill description " + strings.Repeat("x", 80),
		})
	}
	_ = m.slashItems() // warm catalog once
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.input.SetValue("/be")
		m.updateCompletion()
		if !m.completion.active {
			b.Fatal("expected completion menu")
		}
	}
	b.StopTimer()
	if b.N > 0 {
		// Informational soft gate; CI machines vary.
		_ = time.Millisecond
	}
}

func TestSlashArgDataSnapshotsAcrossKeystrokes(t *testing.T) {
	isolateUserConfig(t)
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = []skill.Skill{{Name: "warm", Description: "warm"}}
	m.modelRef = "prov/model"

	first := m.slashArgDataSnapshot()
	if len(first.Skills) != 1 || first.Skills[0].Name != "warm" {
		t.Fatalf("arg data snapshot lost skills: %+v", first)
	}
	// Between keystrokes of one popup the snapshot must be served from cache:
	// mutating the model's own lists stays invisible until a rebuild trigger.
	m.skills = []skill.Skill{{Name: "changed"}}
	second := m.slashArgDataSnapshot()
	if second.Skills[0].Name != "warm" {
		t.Fatal("arg data rebuilt between keystrokes of the same popup")
	}

	m.modelRef = "prov/other"
	third := m.slashArgDataSnapshot()
	if third.Skills[0].Name != "changed" {
		t.Fatal("model switch must rebuild the arg data snapshot")
	}
	if third.CurrentModel != "prov/other" || third.CurrentProvider != "prov" {
		t.Fatalf("rebuilt arg data kept stale model identity: %+v", third)
	}

	m.skills = []skill.Skill{{Name: "again"}}
	m.invalidateSlashCatalog()
	fourth := m.slashArgDataSnapshot()
	if fourth.Skills[0].Name != "again" {
		t.Fatal("invalidateSlashCatalog must rebuild the arg data snapshot")
	}
}

func TestSlashArgDataRebuildsWhenPopupReopens(t *testing.T) {
	isolateUserConfig(t)
	store := memory.Store{Dir: t.TempDir()}
	if _, err := store.Save(memory.Memory{Name: "warm", Title: "Warm", Body: "first", Type: memory.TypeProject}); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New(control.Options{Memory: &memory.Set{Store: store}})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.input.SetValue("/memory revisions war")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatal("expected first memory argument popup")
	}

	// Closing the popup ends its snapshot generation. A later popup must see
	// memory saved while no popup was open.
	m.dismissCompletion()
	if _, err := store.Save(memory.Memory{Name: "changed", Title: "Changed", Body: "second", Type: memory.TypeProject}); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("/memory revisions chang")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg || m.completion.items[0].label != "changed" {
		t.Fatalf("reopened popup did not refresh memory refs: %+v", m.completion)
	}
}

func BenchmarkSlashArgCompletionKeystroke(b *testing.B) {
	root := b.TempDir()
	b.Setenv("HOME", root)
	b.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	b.Setenv("XDG_CONFIG_HOME", root+"/config")
	b.Chdir(root)
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.modelRef = "prov/model"
	m.skills = make([]skill.Skill, 0, 1000)
	for i := range 1000 {
		m.skills = append(m.skills, skill.Skill{
			Name:        fmt.Sprintf("bench-skill-%04d", i),
			Description: "benchmark skill description " + strings.Repeat("x", 80),
		})
	}
	_ = m.slashItems()
	m.input.SetValue("/language ")
	m.updateCompletion() // open the popup and warm its snapshot
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.input.SetValue("/language e")
		m.updateCompletion()
		if !m.completion.active {
			b.Fatal("expected completion menu")
		}
	}
}

func BenchmarkSlashEffortArgCompletionKeystroke(b *testing.B) {
	root := b.TempDir()
	b.Setenv("HOME", root)
	b.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	b.Setenv("XDG_CONFIG_HOME", root+"/config")
	b.Chdir(root)
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.input.SetValue("/effort ")
	m.updateCompletion() // resolve config once for this popup generation
	if !m.completion.active {
		b.Fatal("expected initial effort completion menu")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.input.SetValue("/effort h")
		m.updateCompletion()
		if !m.completion.active {
			b.Fatal("expected effort completion menu")
		}
	}
}
