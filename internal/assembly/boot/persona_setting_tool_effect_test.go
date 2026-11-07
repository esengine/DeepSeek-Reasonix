package boot

import (
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestEffectSettingToolCannotChangeOrDiscloseUserPersona(t *testing.T) {
	for _, tc := range []struct{ id, value string }{
		{"agent.persona.prefix", `"replacement prefix"`},
		{"agent.persona.suffix", `"replacement suffix"`},
		{"agent.persona.include_runtime_context", `false`},
	} {
		t.Run(tc.id, func(t *testing.T) {
			rec, ctrl, userFile, approvals := settingSession(t, tc.id, tc.value)
			include := true
			before := config.PersonaConfig{Prefix: "private user prefix", Suffix: "private user suffix", IncludeRuntimeContext: &include}
			cfg := config.LoadForEdit(userFile)
			cfg.Agent.Persona = before
			if err := cfg.SaveTo(userFile); err != nil {
				t.Fatal(err)
			}
			if err := <-runSetting(ctrl); err != nil {
				t.Fatalf("Run: %v", err)
			}
			select {
			case card := <-approvals:
				t.Fatalf("persona proposal offered an approval card: %+v", card)
			default:
			}
			result := rec.toolResult()
			for _, want := range []string{"setting.guarded", "(fix: tool:setting op=open id=" + tc.id + ")"} {
				if !strings.Contains(result, want) {
					t.Fatalf("tool result = %q, want %q", result, want)
				}
			}
			if strings.Contains(result, before.Prefix) || strings.Contains(result, before.Suffix) {
				t.Fatal("persona refusal disclosed the user's value")
			}
			if got := config.LoadForEdit(userFile).Agent.Persona; !reflect.DeepEqual(got, before) {
				t.Fatalf("guarded persona changed: %+v", got)
			}
			for _, row := range ctrl.SettingsList("agent.persona") {
				if strings.HasPrefix(row.ID, "agent.persona.") && (row.Value != nil || row.Class != "guarded") {
					t.Fatalf("persona list disclosed a boundary value: %+v", row)
				}
			}
		})
	}
}
