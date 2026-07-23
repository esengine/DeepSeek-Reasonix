package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRepairPlanRejectsUnknownFieldsAndActions(t *testing.T) {
	tests := []string{
		`{"schemaVersion":1,"summary":"x","actions":[{"type":"run_shell","reason":"x"}]}`,
		`{"schemaVersion":1,"summary":"x","actions":[{"type":"rollback_update","reason":"x","command":"rm"}]}`,
		`{"schemaVersion":1,"summary":"x","actions":[{"type":"rebuild_derived_state","target":"sessions","reason":"x"}]}`,
		`{"schemaVersion":1,"summary":"\u001b[2J","actions":[]}`,
	}
	for _, raw := range tests {
		if _, err := DecodeRepairPlan([]byte(raw)); err == nil {
			t.Fatalf("unsafe plan accepted: %s", raw)
		}
	}
}

func TestDecodeRepairPlanAcceptsFencedWhitelistPlan(t *testing.T) {
	raw := "```json\n" + `{"schemaVersion":1,"summary":"repair tabs","actions":[{"type":"rebuild_derived_state","target":"tabs","reason":"malformed"}]}` + "\n```"
	plan, err := DecodeRepairPlan([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Target != "tabs" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDecodeRepairPlanAllowsNoOpPlan(t *testing.T) {
	plan, err := DecodeRepairPlan([]byte(`{"schemaVersion":1,"summary":"no safe repair","actions":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %+v", plan.Actions)
	}
}

func TestProjectRepairPlanRequiresExplicitPermission(t *testing.T) {
	plan := RepairPlan{SchemaVersion: 1, Summary: "project", Actions: []RepairPlanAction{{Type: "repair_config", Scope: "project", Reason: "bad toml"}}}
	if _, err := PreviewRepairPlan(plan, ApplyPlanOptions{Root: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "--allow-project") {
		t.Fatalf("preview error = %v", err)
	}
}

func TestApplyRepairPlanMultiActionUndoRevertsWholePlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	global := filepath.Join(home, "config.toml")
	tabs := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(global, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tabs, []byte("bad-tabs"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := RepairPlan{SchemaVersion: 1, Summary: "config + tabs", Actions: []RepairPlanAction{
		{Type: "repair_config", Scope: "global", Reason: "bad toml"},
		{Type: "rebuild_derived_state", Target: "tabs", Reason: "bad tabs"},
	}}
	if _, err := ApplyRepairPlan(plan, ApplyPlanOptions{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tabs); !os.IsNotExist(err) {
		t.Fatalf("tabs not quarantined: %v", err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(global)
	if err != nil || string(got) != "[broken\n" {
		t.Fatalf("global config not restored by plan-level undo: %q, %v", got, err)
	}
	got, err = os.ReadFile(tabs)
	if err != nil || string(got) != "bad-tabs" {
		t.Fatalf("derived state not restored by plan-level undo: %q, %v", got, err)
	}
}

func TestProjectRepairPlanDoesNotRepairGlobalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	global := filepath.Join(home, "config.toml")
	project := filepath.Join(root, "reasonix.toml")
	for _, path := range []string{global, project} {
		if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan := RepairPlan{SchemaVersion: 1, Summary: "project only", Actions: []RepairPlanAction{{Type: "repair_config", Scope: "project", Reason: "bad project toml"}}}
	if _, err := ApplyRepairPlan(plan, ApplyPlanOptions{Root: root, AllowProject: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(global); err != nil {
		t.Fatalf("global config was touched: %v", err)
	}
	if _, err := os.Stat(project); !os.IsNotExist(err) {
		t.Fatalf("project config was not quarantined: %v", err)
	}
}
