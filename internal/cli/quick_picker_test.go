package cli

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

func TestQuickPickerNavigationFilterAndConfirm(t *testing.T) {
	p := &quickPicker{
		title: "Select model",
		items: []quickPickerItem{
			{ID: "one", Label: "Alpha"},
			{ID: "two", Label: "Beta model"},
			{ID: "three", Label: "Gamma"},
		},
	}
	p.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.selected != 1 {
		t.Fatalf("down selected = %d, want 1", p.selected)
	}
	p.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 0 {
		t.Fatalf("up selected = %d, want 0", p.selected)
	}
	p.handleKey(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if got := p.filteredItems(); len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("filtered items = %+v, want Beta only", got)
	}
	result := p.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if result.choice == nil || result.choice.ID != "two" {
		t.Fatalf("enter choice = %+v, want two", result.choice)
	}
}

func TestQuickPickerVimKeysBecomeTextAfterSearchStarts(t *testing.T) {
	p := &quickPicker{
		selected: 1,
		items: []quickPickerItem{
			{ID: "one", Label: "Alpha"},
			{ID: "two", Label: "Beta"},
			{ID: "three", Label: "Gamma"},
		},
	}
	p.handleKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if p.selected != 0 || p.query != "" {
		t.Fatalf("empty-query k = selected %d, query %q; want navigation to 0", p.selected, p.query)
	}
	p.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if p.selected != 1 || p.query != "" {
		t.Fatalf("empty-query j = selected %d, query %q; want navigation to 1", p.selected, p.query)
	}

	p.handleKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	p.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.handleKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if p.query != "ajk" {
		t.Fatalf("search query = %q, want ajk", p.query)
	}
}

func TestQuickPickerWindowTracksSelection(t *testing.T) {
	start, end := quickPickerWindow(20, 18)
	if start != 12 || end != 20 {
		t.Fatalf("window = [%d,%d), want [12,20)", start, end)
	}
}

func TestQuickPickerRendersWithinNarrowPanel(t *testing.T) {
	p := &quickPicker{
		title: "Select model",
		items: []quickPickerItem{{
			ID: "long", Label: strings.Repeat("very-long-model-name-", 5),
			Description: strings.Repeat("provider description ", 5), Status: "active",
		}},
	}
	out := p.render(32)
	for _, line := range strings.Split(out, "\n") {
		if got := visibleWidth(line); got > 32 {
			t.Fatalf("rendered line width = %d, want <= 32: %q", got, line)
		}
	}
}

func TestQuickPickerEscCancels(t *testing.T) {
	p := &quickPicker{items: []quickPickerItem{{ID: "one", Label: "One"}}}
	if result := p.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc}); !result.cancelled {
		t.Fatal("Esc should cancel picker")
	}
}

// TestQuickPickerModelSwitch verifies that selecting a model from the
// quickPicker returns a non-nil tea.Cmd that produces a modelSwitchMsg.
// Regression test for the bug where handleQuickPickerKey set
// m.pendingModelSwitch but returned nil as the tea.Cmd, causing the async
// controller build to never execute.
func TestQuickPickerModelSwitch(t *testing.T) {
	oldCtrl := control.New(control.Options{Label: "old"})
	newCtrl := control.New(control.Options{Label: "new-model", Commands: []command.Command{{Name: "cmd"}}, Skills: []skill.Skill{{Name: "sk"}}})
	m := newChatTUI(oldCtrl, "", make(chan event.Event, 1), 100)
	m.modelRef = "provider/old-model"
	m.quickPick = &quickPicker{
		kind:  quickPickerModel,
		title: "Select model",
		items: []quickPickerItem{
			{ID: "provider/new-model", Label: "provider/new-model"},
		},
		selected: 0,
	}
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, _ string, _ control.SessionAPI) (*control.Controller, error) {
		return newCtrl, nil
	}

	// Simulate pressing Enter in the quickPicker
	next, cmd := m.handleQuickPickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := next.(chatTUI)

	if cmd == nil {
		t.Fatal("quickPick model switch did not schedule a controller build (cmd is nil)")
	}
	if !m2.modelSwitchPending {
		t.Fatal("quickPick model switch did not set modelSwitchPending")
	}
	if m2.pendingModelSwitch == nil {
		t.Fatal("quickPick model switch did not set pendingModelSwitch")
	}
	if m2.ctrl != oldCtrl {
		t.Fatal("controller changed before the replacement build completed")
	}

	// Execute the cmd and verify it produces a modelSwitchMsg
	msg := cmd()
	if msg == nil {
		t.Fatal("controller build cmd returned nil message")
	}
	swMsg, ok := msg.(modelSwitchMsg)
	if !ok {
		t.Fatalf("controller build returned %T, want modelSwitchMsg", msg)
	}
	if swMsg.err != nil {
		t.Fatalf("controller build failed: %v", swMsg.err)
	}
	if swMsg.ref != "provider/new-model" {
		t.Fatalf("modelSwitchMsg ref = %q, want %q", swMsg.ref, "provider/new-model")
	}
	if swMsg.ctrl != newCtrl {
		t.Fatal("modelSwitchMsg did not carry the new controller")
	}
	if swMsg.oldCtrl != oldCtrl {
		t.Fatal("modelSwitchMsg did not carry the old controller")
	}
}

func TestQuickPickerProviderSingleModelSwitch(t *testing.T) {
	isolateUserConfig(t)
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{
		Name:    "new-provider",
		Kind:    "openai",
		BaseURL: "http://localhost:1234/v1",
		Model:   "new-model",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save provider config: %v", err)
	}

	oldCtrl := control.New(control.Options{Label: "old"})
	newCtrl := control.New(control.Options{Label: "new-model"})
	m := newChatTUI(oldCtrl, "", make(chan event.Event, 1), 100)
	m.modelRef = "old-provider/old-model"
	m.quickPick = &quickPicker{
		kind:     quickPickerProvider,
		title:    "Select provider",
		items:    []quickPickerItem{{ID: "new-provider", Label: "new-provider"}},
		selected: 0,
	}
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, _ string, _ control.SessionAPI) (*control.Controller, error) {
		return newCtrl, nil
	}

	next, cmd := m.handleQuickPickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := next.(chatTUI)
	if cmd == nil {
		t.Fatal("provider quickPick did not schedule a controller build (cmd is nil)")
	}
	if !m2.modelSwitchPending || m2.pendingModelSwitch == nil {
		t.Fatal("provider quickPick did not retain the pending model switch")
	}

	msg := cmd()
	swMsg, ok := msg.(modelSwitchMsg)
	if !ok {
		t.Fatalf("controller build returned %T, want modelSwitchMsg", msg)
	}
	if swMsg.err != nil {
		t.Fatalf("controller build failed: %v", swMsg.err)
	}
	if swMsg.ref != "new-provider/new-model" {
		t.Fatalf("modelSwitchMsg ref = %q, want %q", swMsg.ref, "new-provider/new-model")
	}
	if swMsg.ctrl != newCtrl || swMsg.oldCtrl != oldCtrl {
		t.Fatal("modelSwitchMsg did not carry the expected controllers")
	}
}

// TestQuickPickerScrollbarClickDragRelease proves the quick picker's sheet
// scrollbar supports click-drag scrolling, sharing the bottom-sheet drag
// state machine with the completion menu.
func TestQuickPickerScrollbarClickDragRelease(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	items := make([]quickPickerItem, 20)
	for i := range items {
		items[i] = quickPickerItem{ID: fmt.Sprintf("m%d", i), Label: fmt.Sprintf("model-%d", i)}
	}
	m.quickPick = &quickPicker{kind: quickPickerModel, title: "Select model", items: items, selected: 0}
	barX := m.contentWidth() - 1
	rows := m.quickPickerItemRows()
	top, _ := m.bottomSheetBounds(rows + m.quickPickerHeaderRows() + 1)

	m0, _ = m.Update(tea.MouseClickMsg{X: barX, Y: top + m.quickPickerHeaderRows() + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.sheetScrollbar == nil || m.sheetScrollbar.panel != "quick" {
		t.Fatal("left-click on the quick picker scrollbar should start a drag")
	}
	if m.quickPick.selected == 0 {
		t.Fatal("clicking mid-scrollbar should move the quick picker selection")
	}

	// Drag to the sheet bottom reaches the end of the list.
	bottom := top + m.quickPickerHeaderRows() + rows - 1
	m0, _ = m.Update(tea.MouseMotionMsg{X: barX, Y: bottom, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	windowTop := completionWindowStart(m.quickPick.selected, len(items), rows)
	if want := len(items) - rows; windowTop != want {
		t.Fatalf("dragging to the bottom should reach the list end, windowTop=%d want %d (sel=%d)", windowTop, want, m.quickPick.selected)
	}

	m0, _ = m.Update(tea.MouseReleaseMsg{X: barX, Y: bottom, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.sheetScrollbar != nil {
		t.Fatal("mouse release should end the quick picker scrollbar drag")
	}
}

// TestQuickPickerWheelScrollsSheet proves the wheel over the quick picker
// moves its selection instead of the transcript.
func TestQuickPickerWheelScrollsSheet(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	items := make([]quickPickerItem, 12)
	for i := range items {
		items[i] = quickPickerItem{ID: fmt.Sprintf("m%d", i), Label: fmt.Sprintf("model-%d", i)}
	}
	m.quickPick = &quickPicker{kind: quickPickerModel, title: "Select model", items: items, selected: 0}
	rows := m.quickPickerItemRows()
	top, _ := m.bottomSheetBounds(rows + m.quickPickerHeaderRows() + 1)

	m0, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: top + 1, Button: tea.MouseWheelDown})
	m = m0.(chatTUI)
	if m.quickPick.selected != 1 {
		t.Fatalf("wheel down over the quick picker should move selection, sel=%d", m.quickPick.selected)
	}
}
