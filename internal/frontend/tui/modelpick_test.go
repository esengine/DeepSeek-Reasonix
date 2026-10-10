package tui

import (
	"strings"
	"testing"
)

func submitLine(m *model, line string) {
	m.composer.SetValue(line)
	run(m, press(m, "enter"))
}

// /model opens a searchable panel on the kernel's chat models, the active one
// under the cursor; Enter switches the session to the row chosen.
func TestModelOpensAPickerAndSwitches(t *testing.T) {
	m, k := testModel(t)
	submitLine(m, "/model")
	if m.quick == nil || m.quick.sel != 0 {
		t.Fatalf("quick = %+v", m.quick)
	}
	if v := m.View().Content; !strings.Contains(v, "alpha/deep") || !strings.Contains(v, "(active)") {
		t.Fatalf("panel missing rows:\n%s", v)
	}
	typeText(m, "solo")
	if got := m.quick.shown(); len(got) != 1 || got[0].ID != "beta/solo" {
		t.Fatalf("filter = %+v", got)
	}
	run(m, press(m, "enter"))
	calls := strings.Join(k.seen(), "\n")
	if m.quick != nil || !strings.Contains(calls, `POST /model {"ref":"beta/solo"}`) {
		t.Fatalf("switch not sent:\n%s", calls)
	}
	if strings.Contains(calls, "POST /submit") {
		t.Fatal("/model went to the kernel as a prompt")
	}
}

func TestModelWithARefSwitchesWithoutAPanel(t *testing.T) {
	m, k := testModel(t)
	submitLine(m, "/model alpha/deep")
	if m.quick != nil || !strings.Contains(strings.Join(k.seen(), "\n"), `POST /model {"ref":"alpha/deep"}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
}

func TestModelRefusesWhileATurnRuns(t *testing.T) {
	m, k := testModel(t)
	m.tr.Running = true
	submitLine(m, "/model alpha/deep")
	if strings.Contains(strings.Join(k.seen(), "\n"), "POST /model") {
		t.Fatal("switched mid-turn")
	}
}

// /provider lists providers; a provider with one model switches at once, one
// with several opens its models.
func TestProviderPicksThenModel(t *testing.T) {
	m, k := testModel(t)
	submitLine(m, "/provider")
	if m.quick == nil || len(m.quick.items) != 2 || !m.quick.items[0].Active {
		t.Fatalf("providers = %+v", m.quick)
	}
	run(m, press(m, "enter"))
	if m.quick == nil || len(m.quick.items) != 2 || m.quick.items[1].ID != "alpha/deep" {
		t.Fatalf("models of alpha = %+v", m.quick)
	}
	run(m, press(m, "down"))
	run(m, press(m, "enter"))
	if !strings.Contains(strings.Join(k.seen(), "\n"), `POST /model {"ref":"alpha/deep"}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
	submitLine(m, "/provider beta")
	if !strings.Contains(strings.Join(k.seen(), "\n"), `POST /model {"ref":"beta/solo"}`) {
		t.Fatalf("single-model provider did not switch:\n%s", strings.Join(k.seen(), "\n"))
	}
}

func TestModelPanelEscapeCloses(t *testing.T) {
	m, _ := testModel(t)
	submitLine(m, "/model")
	run(m, press(m, "esc"))
	if m.quick != nil {
		t.Fatal("esc left the panel open")
	}
}
